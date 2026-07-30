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

	store.SetMemoForMessage(1, 101, "memos/abc123")
	store.SetMemoForMessage(1, 202, "memos/def456")

	reloaded := NewStore(dataPath)
	if err := reloaded.Init(); err != nil {
		t.Fatalf("init reloaded store: %v", err)
	}

	memoName, ok := reloaded.GetMemoForMessage(1, 101)
	if !ok || memoName != "memos/abc123" {
		t.Fatalf("expected memos/abc123 for message 101, got %q", memoName)
	}

	memoName, ok = reloaded.GetMemoForMessage(1, 202)
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

	if _, ok := store.GetMemoForMessage(1, 999); ok {
		t.Fatalf("expected no mapping for message 999")
	}
}

func TestMessageMemoMapScopedByChat(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "data.txt")

	store := NewStore(dataPath)
	if err := store.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}

	// Same message ID in two different chats must not collide: Telegram
	// message IDs are only unique within a chat.
	store.SetMemoForMessage(1, 5, "memos/chat-one")
	store.SetMemoForMessage(2, 5, "memos/chat-two")

	memoName, ok := store.GetMemoForMessage(1, 5)
	if !ok || memoName != "memos/chat-one" {
		t.Fatalf("expected memos/chat-one for chat 1 message 5, got %q", memoName)
	}

	memoName, ok = store.GetMemoForMessage(2, 5)
	if !ok || memoName != "memos/chat-two" {
		t.Fatalf("expected memos/chat-two for chat 2 message 5, got %q", memoName)
	}
}
