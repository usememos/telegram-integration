package memogram

import (
	"testing"

	"github.com/usememos/memogram/internal/memosapi"
)

func TestBuildMemoSearchFilterUsesUsernameResourceName(t *testing.T) {
	got := buildMemoSearchFilter("needle", &memosapi.User{
		Name:     "users/alice",
		Username: "alice",
	})
	want := `content.contains("needle") && creator == "users/alice"`
	if got != want {
		t.Fatalf("unexpected filter:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestBuildMemoSearchFilterFallsBackToUsername(t *testing.T) {
	got := buildMemoSearchFilter("needle", &memosapi.User{
		Username: "alice",
	})
	want := `content.contains("needle") && creator == "users/alice"`
	if got != want {
		t.Fatalf("unexpected filter:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestBuildMemoSearchFilterEscapesSearchString(t *testing.T) {
	got := buildMemoSearchFilter(`quote " test`, &memosapi.User{Name: "users/alice"})
	want := `content.contains("quote \" test") && creator == "users/alice"`
	if got != want {
		t.Fatalf("unexpected filter:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestBuildMemoSearchFilterAllowsUnknownUser(t *testing.T) {
	got := buildMemoSearchFilter("needle", nil)
	want := `content.contains("needle")`
	if got != want {
		t.Fatalf("unexpected filter:\nwant: %q\ngot:  %q", want, got)
	}
}
