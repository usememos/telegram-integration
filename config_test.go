package memogram

import (
	"testing"

	v1pb "github.com/usememos/memos/proto/gen/api/v1"
)

func TestDefaultMemoVisibilityUnset(t *testing.T) {
	visibility, ok, err := (&Config{}).defaultMemoVisibility()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected unset visibility")
	}
	if visibility != v1pb.Visibility_VISIBILITY_UNSPECIFIED {
		t.Fatalf("unexpected visibility: %v", visibility)
	}
}

func TestDefaultMemoVisibilityValues(t *testing.T) {
	tests := map[string]v1pb.Visibility{
		"private":     v1pb.Visibility_PRIVATE,
		"protected":   v1pb.Visibility_PROTECTED,
		"public":      v1pb.Visibility_PUBLIC,
		" Protected ": v1pb.Visibility_PROTECTED,
	}

	for value, want := range tests {
		visibility, ok, err := (&Config{DefaultVisibility: value}).defaultMemoVisibility()
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", value, err)
		}
		if !ok {
			t.Fatalf("expected visibility to be set for %q", value)
		}
		if visibility != want {
			t.Fatalf("unexpected visibility for %q: want %v, got %v", value, want, visibility)
		}
	}
}

func TestDefaultMemoVisibilityRejectsInvalidValue(t *testing.T) {
	_, _, err := (&Config{DefaultVisibility: "friends"}).defaultMemoVisibility()
	if err == nil {
		t.Fatal("expected invalid visibility error")
	}
}
