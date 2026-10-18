package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestParseLine(t *testing.T) {
	userID, u, ok := parseLine("123:abc:def")
	if !ok || userID != 123 || u.token != "abc:def" {
		t.Fatalf("legacy line: got %d, %+v, %v", userID, u, ok)
	}
	userID, u, ok = parseLine(`{"user":7,"token":"t:1","space":"spaces/team","spaceTitle":"Team <A&B>"}`)
	if !ok || userID != 7 || u != (user{token: "t:1", space: Space{Name: "spaces/team", Title: "Team <A&B>"}}) {
		t.Fatalf("JSON line: got %d, %+v, %v", userID, u, ok)
	}
	for _, line := range []string{"123", "123:", "abc:token", "0:token", ":token", `{"user":0,"token":"t"}`, `{"user":1}`, `{"user":1,"token":`} {
		if _, _, ok := parseLine(line); ok {
			t.Errorf("accepted malformed line %q", line)
		}
	}
}

func TestSaveAndLoadUserAccessTokens(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "data.txt")
	store, err := Open(dataPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if info, err := os.Stat(dataPath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("data file should be created with mode 0600: %v, %v", info, err)
	}
	if err := store.SetAccessToken(42, "token-one"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAccessToken(7, "token:two"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Open(dataPath)
	if err != nil {
		t.Fatalf("open reloaded store: %v", err)
	}
	for userID, want := range map[int64]string{42: "token-one", 7: "token:two"} {
		if token, ok := reloaded.AccessToken(userID); !ok || token != want {
			t.Fatalf("user %d: got %q, want %q", userID, token, want)
		}
	}
	data, err := os.ReadFile(dataPath)
	if err != nil || string(data) != `{"user":7,"token":"token:two"}`+"\n"+`{"user":42,"token":"token-one"}`+"\n" {
		t.Fatalf("unexpected data file: %q, %v", data, err)
	}
}

func TestOpenSkipsCommentsAndMalformedLines(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "data.txt")
	if err := os.WriteFile(dataPath, []byte("# comment\n\n1:one\nbroken\n2:two\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if token, _ := store.AccessToken(2); token != "two" {
		t.Fatalf("got %q", token)
	}
}

func TestOpenRejectsDirectory(t *testing.T) {
	if _, err := Open(t.TempDir()); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("got %v", err)
	}
}

func TestConcurrentBindingsAreAllSaved(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "data.txt")
	store, err := Open(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := int64(1); i <= 20; i++ {
		wg.Go(func() {
			if err := store.SetAccessToken(i, fmt.Sprint("token-", i)); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	reloaded, err := Open(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 20; i++ {
		if _, ok := reloaded.AccessToken(i); !ok {
			t.Fatalf("binding for user %d was lost", i)
		}
	}
}

func TestFailedSaveKeepsPreviousBinding(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "data.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAccessToken(1, "old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if err := store.SetAccessToken(1, "new"); err == nil {
		t.Skip("directory permissions are not enforced for this user")
	}
	if err := store.SetAccessToken(2, "added"); err == nil {
		t.Fatal("expected save failure")
	}
	if token, _ := store.AccessToken(1); token != "old" {
		t.Fatalf("failed save replaced binding with %q", token)
	}
	if _, ok := store.AccessToken(2); ok {
		t.Fatal("failed save kept a new binding")
	}
}

func TestLegacyFileIsMigratedOnNextSave(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "data.txt")
	if err := os.WriteFile(dataPath, []byte("# legacy\n42:token:with:colons\n7:seven\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if token, _ := store.AccessToken(42); token != "token:with:colons" {
		t.Fatalf("legacy token: %q", token)
	}
	if err := store.SetDefaultSpace(7, Space{Name: "spaces/team", Title: "Team"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dataPath)
	want := `{"user":7,"token":"seven","space":"spaces/team","spaceTitle":"Team"}` + "\n" + `{"user":42,"token":"token:with:colons"}` + "\n"
	if err != nil || string(data) != want {
		t.Fatalf("migrated file:\n%s\nwant:\n%s", data, want)
	}
	if info, err := os.Stat(dataPath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("migrated file must stay private: %v, %v", info, err)
	}
}

func TestDefaultSpace(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "data.txt")
	store, err := Open(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	team := Space{Name: "spaces/team", Title: "Team"}
	if err := store.SetDefaultSpace(1, team); !errors.Is(err, ErrNotBound) {
		t.Fatalf("unbound user: %v", err)
	}
	if store.DefaultSpace(1) != (Space{}) {
		t.Fatal("unbound user has a default space")
	}
	if err := store.SetAccessToken(1, "old"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetDefaultSpace(1, team); err != nil {
		t.Fatal(err)
	}
	// Rotating the token keeps the user's settings.
	if err := store.SetAccessToken(1, "new"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Open(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if token, _ := reloaded.AccessToken(1); token != "new" || reloaded.DefaultSpace(1) != team {
		t.Fatalf("got token %q, space %+v", token, reloaded.DefaultSpace(1))
	}
	if err := reloaded.SetDefaultSpace(1, Space{}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(dataPath); string(data) != `{"user":1,"token":"new"}`+"\n" {
		t.Fatalf("cleared space was saved: %s", data)
	}
}

func TestFailedSaveKeepsPreviousDefaultSpace(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(filepath.Join(dir, "data.txt"))
	if err != nil {
		t.Fatal(err)
	}
	team := Space{Name: "spaces/team", Title: "Team"}
	if err := store.SetAccessToken(1, "token"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetDefaultSpace(1, team); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if err := store.SetDefaultSpace(1, Space{Name: "spaces/other"}); err == nil {
		t.Skip("directory permissions are not enforced for this user")
	}
	if store.DefaultSpace(1) != team {
		t.Fatalf("failed save changed the default space to %+v", store.DefaultSpace(1))
	}
}
