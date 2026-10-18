package bot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memosapi"
)

func TestCallbackDataRoundTrip(t *testing.T) {
	for _, action := range []memoAction{actionPublic, actionProtected, actionPrivate, actionPin} {
		data := callbackData{action: action, memo: "memos/" + strings.Repeat("x", 36)}
		if len(data.String()) > 64 {
			t.Fatalf("%s exceeds Telegram's 64 byte callback limit", data)
		}
		got, err := parseCallbackData(data.String())
		if err != nil || got != data || !got.action.valid() {
			t.Fatalf("round trip of %q: %+v, %v", data, got, err)
		}
	}
	for _, raw := range []string{"", "pin", "pin memos/a extra"} {
		if _, err := parseCallbackData(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	if memoAction("delete").valid() {
		t.Error("unknown action is valid")
	}
}

func TestKeyboardKeepsLegacyCallbackFormat(t *testing.T) {
	// Buttons on messages sent by earlier versions must keep working.
	s := newTestService(t, newFakeTelegram(t, nil), Options{})
	keyboard := s.memoKeyboard(&memosapi.Memo{Name: "memos/abc"})
	if want := "Public=public memos/abc,Private=private memos/abc,Pin=pin memos/abc|Space=space memos/abc"; keyboardLayout(keyboard) != want {
		t.Fatalf("got %s", keyboardLayout(keyboard))
	}
	// Servers without spaces get no Space button.
	s.spaces.Store(false)
	if want := "Public=public memos/abc,Private=private memos/abc,Pin=pin memos/abc"; keyboardLayout(s.memoKeyboard(&memosapi.Memo{Name: "memos/abc"})) != want {
		t.Fatalf("got %s", keyboardLayout(s.memoKeyboard(&memosapi.Memo{Name: "memos/abc"})))
	}
}

func TestCallback(t *testing.T) {
	for _, tc := range []struct {
		name, data, wantAnswer string
		alert, edited          bool
		inaccessible           bool
	}{
		{name: "pin", data: "pin memos/abc", wantAnswer: "Memo updated", edited: true},
		{name: "inaccessible message", data: "public memos/abc", wantAnswer: "Memo updated", inaccessible: true},
		{name: "malformed", data: "pin", wantAnswer: "Invalid command", alert: true},
		{name: "unknown action", data: "delete memos/abc", wantAnswer: "Unknown action", alert: true},
		{name: "missing memo", data: "pin memos/missing", wantAnswer: "Memo memos/missing not found", alert: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tg := newFakeTelegram(t, nil)
			data := openStore(t)
			bindUser(t, data, 1, "token")
			var updated memosapi.Memo
			s := newTestService(t, tg, Options{
				Store: data,
				Memos: fakeMemos(t, map[string]http.HandlerFunc{
					"GET /api/memos/abc": respond(`{"name":"memos/abc","content":"note","visibility":"PRIVATE","state":"NORMAL"}`),
					"GET /api/memos/missing": func(w http.ResponseWriter, _ *http.Request) {
						w.WriteHeader(http.StatusNotFound)
					},
					"PATCH /api/memos/abc": func(w http.ResponseWriter, r *http.Request) {
						if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
							t.Error(err)
						}
						updated.Name = "memos/abc"
						_ = json.NewEncoder(w).Encode(updated)
					},
				}),
			})
			message := models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeMessage, Message: textMessage(1, "")}
			if tc.inaccessible {
				message = models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeInaccessibleMessage, InaccessibleMessage: &models.InaccessibleMessage{}}
			}
			s.process(&models.Update{CallbackQuery: &models.CallbackQuery{ID: "q", From: models.User{ID: 1}, Data: tc.data, Message: message}})

			calls := tg.snapshot()
			want := "answerCallbackQuery"
			if tc.edited {
				want = "editMessageText,answerCallbackQuery"
			}
			if got := methods(calls); got != want {
				t.Fatalf("calls %s, want %s", got, want)
			}
			answer := calls[len(calls)-1]
			if answer.text != tc.wantAnswer || answer.alert != tc.alert {
				t.Fatalf("answer %+v, want %q alert=%v", answer, tc.wantAnswer, tc.alert)
			}
			if tc.edited && (!updated.Pinned || calls[0].text != fmt.Sprintf("Memo updated as PRIVATE with [memos/abc](%s/memos/abc) 📌", s.memos.BaseURL())) {
				t.Fatalf("pin was not applied or confirmed: %+v, %q", updated, calls[0].text)
			}
		})
	}
}
