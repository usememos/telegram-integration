package bot

import (
	"errors"
	"net/http"
	"testing"

	"github.com/go-telegram/bot/models"
)

type failingStore struct{ UserStore }

func (failingStore) SetAccessToken(int64, string) error { return errors.New("disk full") }

func TestStartReportsSaveFailure(t *testing.T) {
	tg := newFakeTelegram(t, nil)
	data := failingStore{openStore(t)}
	s := newTestService(t, tg, Options{
		Memos: fakeMemos(t, map[string]http.HandlerFunc{"GET /api/auth/me": respond(`{"user":{"name":"users/alice","displayName":"Alice"}}`)}),
		Store: data,
	})
	s.process(&models.Update{Message: textMessage(1, "/start token")})
	if calls := tg.snapshot(); len(calls) != 1 || calls[0].text != "Failed to save access token, please try again" {
		t.Fatalf("unexpected replies: %+v", calls)
	}
	if _, ok := data.AccessToken(1); ok {
		t.Fatal("failed save bound the user")
	}
}

func TestSearchReportsFailure(t *testing.T) {
	tg := newFakeTelegram(t, nil)
	data := openStore(t)
	bindUser(t, data, 1, "token")
	s := newTestService(t, tg, Options{
		Memos: fakeMemos(t, map[string]http.HandlerFunc{
			"GET /api/auth/me": respond(`{"user":{"name":"users/alice"}}`),
			"GET /api/memos": func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		}),
		Store: data,
	})
	s.process(&models.Update{Message: textMessage(1, "/search needle")})
	if calls := tg.snapshot(); len(calls) != 1 || calls[0].text != "Failed to search memos" {
		t.Fatalf("unexpected replies: %+v", calls)
	}
}

func TestCommandsRequireExactName(t *testing.T) {
	for text, want := range map[string]bool{"/start": true, "/start token": true, "/starting": false, "start": false, "/search": true, "/searches x": false} {
		update := &models.Update{Message: &models.Message{Text: text}}
		if got := isCommand(commandStart)(update) || isCommand(commandSearch)(update); got != want {
			t.Errorf("%q: got %v, want %v", text, got, want)
		}
	}
}
