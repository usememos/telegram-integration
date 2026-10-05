package memogram

import (
	"errors"
	"testing"

	v1pb "github.com/usememos/memos/proto/gen/api/v1"
)

func TestKeyboardIncludesVisibilityActions(t *testing.T) {
	memo := &v1pb.Memo{Name: "memos/123"}
	keyboard := (&Service{}).keyboard(memo)

	got := map[string]string{}
	for _, row := range keyboard.InlineKeyboard {
		for _, button := range row {
			got[button.Text] = button.CallbackData
		}
	}

	want := map[string]string{
		"Public":    "public memos/123",
		"Protected": "protected memos/123",
		"Private":   "private memos/123",
	}
	for text, callbackData := range want {
		if got[text] != callbackData {
			t.Fatalf("unexpected callback for %q: want %q, got %q", text, callbackData, got[text])
		}
	}
}

func TestIsSQLiteBusyError(t *testing.T) {
	tests := map[string]bool{
		"internal: failed to update memo: database is locked (5) (SQLITE_BUSY)": true,
		"internal: failed to update memo: SQLITE_BUSY":                          true,
		"permission denied": false,
	}

	for message, want := range tests {
		if got := isSQLiteBusyError(errors.New(message)); got != want {
			t.Fatalf("unexpected sqlite busy detection for %q: want %v, got %v", message, want, got)
		}
	}

	if isSQLiteBusyError(nil) {
		t.Fatal("nil error should not be sqlite busy")
	}
}
