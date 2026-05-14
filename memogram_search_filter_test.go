package memogram

import (
	"testing"

	v1pb "github.com/usememos/memos/proto/gen/api/v1"
)

func TestBuildMemoSearchFilterUsesUsernameResourceName(t *testing.T) {
	got := buildMemoSearchFilter("needle", &v1pb.User{
		Name:     "users/alice",
		Username: "alice",
	}, false)
	want := `content.contains("needle") && creator == "users/alice"`
	if got != want {
		t.Fatalf("unexpected filter:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestBuildMemoSearchFilterFallsBackToUsername(t *testing.T) {
	got := buildMemoSearchFilter("needle", &v1pb.User{
		Username: "alice",
	}, false)
	want := `content.contains("needle") && creator == "users/alice"`
	if got != want {
		t.Fatalf("unexpected filter:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestBuildMemoSearchFilterEscapesSearchString(t *testing.T) {
	got := buildMemoSearchFilter(`quote " test`, &v1pb.User{Name: "users/alice"}, false)
	want := `content.contains("quote \" test") && creator == "users/alice"`
	if got != want {
		t.Fatalf("unexpected filter:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestBuildMemoSearchFilterAllowsUnknownUser(t *testing.T) {
	got := buildMemoSearchFilter("needle", nil, false)
	want := `content.contains("needle")`
	if got != want {
		t.Fatalf("unexpected filter:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestBuildMemoSearchFilterSearchAll(t *testing.T) {
	got := buildMemoSearchFilter("needle", &v1pb.User{
		Name:     "users/alice",
		Username: "alice",
	}, true)
	want := `content.contains("needle") && visibility == "PUBLIC"`
	if got != want {
		t.Fatalf("unexpected filter:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestBuildMemoSearchFilterSearchAllNoUser(t *testing.T) {
	got := buildMemoSearchFilter("needle", nil, true)
	want := `content.contains("needle") && visibility == "PUBLIC"`
	if got != want {
		t.Fatalf("unexpected filter:\nwant: %q\ngot:  %q", want, got)
	}
}
