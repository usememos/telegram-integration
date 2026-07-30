package store

import (
	"path/filepath"
	"testing"
)

func TestSaveAndLoadMessageMemoMap(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "data.txt")

	store := NewStore(dataPath)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}

	store.SetMemoForMessage(101, "memos/abc123")
	store.SetMemoForMessage(202, "memos/def456")

	reloaded := NewStore(dataPath)
	if err := reloaded.Init(); err != nil {
		t.Fatalf("init reloaded store: %v", err)
	}

	memoName, ok := reloaded.GetMemoForMessage(101)
	if !ok || memoName != "memos/abc123" {
		t.Fatalf("expected memos/abc123 for message 101, got %q", memoName)
	}

	memoName, ok = reloaded.GetMemoForMessage(202)
	if !ok || memoName != "memos/def456" {
		t.Fatalf("expected memos/def456 for message 202, got %q", memoName)
	}
}

func TestGetMemoForMessageMissing(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "data.txt")

	store := NewStore(dataPath)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}

	if _, ok := store.GetMemoForMessage(999); ok {
		t.Fatalf("expected no mapping for message 999")
	}
}
