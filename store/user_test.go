package store

import (
	"path/filepath"
	"testing"
)

func TestParseLine(t *testing.T) {
	userID, token := parseLine("123:abc:def")
	if userID != 123 {
		t.Fatalf("expected userID 123, got %d", userID)
	}
	if token != "abc:def" {
		t.Fatalf("expected token with colon, got %q", token)
	}
}

func TestSaveAndLoadUserAccessTokens(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "data.txt")

	store := NewStore(dataPath)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}

	store.SetUserAccessToken(42, "token-one")
	store.SetUserAccessToken(7, "token:two")

	reloaded := NewStore(dataPath)
	if err := reloaded.Init(); err != nil {
		t.Fatalf("init reloaded store: %v", err)
	}

	token, ok := reloaded.GetUserAccessToken(42)
	if !ok || token != "token-one" {
		t.Fatalf("expected token-one for user 42, got %q", token)
	}

	token, ok = reloaded.GetUserAccessToken(7)
	if !ok || token != "token:two" {
		t.Fatalf("expected token:two for user 7, got %q", token)
	}
}
