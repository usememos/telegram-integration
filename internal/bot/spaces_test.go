package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memos"
	"github.com/usememos/memogram/internal/memosapi"
	"github.com/usememos/memogram/internal/store"
)

const teamSpaces = `{"spaces":[{"name":"spaces/team","title":"Team_A"},{"name":"spaces/home"}]}`

var (
	teamKey = spaceKey("spaces/team")
	homeKey = spaceKey("spaces/home")
)

type memoPatch struct {
	mask string
	body memosapi.Memo
}

// spaceMemos fakes the Memos endpoints that space features use: one memo at
// memos/abc, the user's spaces, and memo creation.
type spaceMemos struct {
	t      *testing.T
	memo   memosapi.Memo
	spaces string // body of GET /api/spaces; empty means the server lacks spaces
	// createStatus returns an error status for a memo creation, or 0.
	createStatus func(memosapi.Memo) int

	spaceLists int
	created    []memosapi.Memo
	patches    []memoPatch
}

func (f *spaceMemos) client() *memos.Client {
	return fakeMemos(f.t, map[string]http.HandlerFunc{
		"GET /api/memos/abc": func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(f.memo)
		},
		"PATCH /api/memos/abc": func(w http.ResponseWriter, r *http.Request) {
			var body memosapi.Memo
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				f.t.Error(err)
			}
			f.patches = append(f.patches, memoPatch{mask: r.URL.Query().Get("updateMask"), body: body})
			body.Name = f.memo.Name
			_ = json.NewEncoder(w).Encode(body)
		},
		"GET /api/spaces": func(w http.ResponseWriter, _ *http.Request) {
			f.spaceLists++
			if f.spaces == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			fmt.Fprint(w, f.spaces)
		},
		"POST /api/memos": func(w http.ResponseWriter, r *http.Request) {
			var body memosapi.Memo
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				f.t.Error(err)
			}
			f.created = append(f.created, body)
			if f.createStatus != nil {
				if status := f.createStatus(body); status != 0 {
					w.WriteHeader(status)
					fmt.Fprint(w, `{"message":"rejected"}`)
					return
				}
			}
			body.Name = fmt.Sprintf("memos/new%d", len(f.created))
			_ = json.NewEncoder(w).Encode(body)
		},
	})
}

// newSpaceService returns a service for a bound user 1 against fake servers.
func newSpaceService(t *testing.T, fake *spaceMemos) (*Service, *fakeTelegram, *store.Store) {
	t.Helper()
	fake.t = t
	if fake.memo.Name == "" {
		fake.memo = memosapi.Memo{Name: "memos/abc", Content: "note", Visibility: memosapi.PRIVATE, State: memosapi.MemoStateNORMAL}
	}
	tg := newFakeTelegram(t, nil)
	data := openStore(t)
	bindUser(t, data, 1, "token")
	return newTestService(t, tg, Options{Memos: fake.client(), Store: data}), tg, data
}

// press sends a button press from user 1 and returns the Telegram calls it
// caused.
func press(s *Service, tg *fakeTelegram, data string) []telegramCall {
	before := len(tg.snapshot())
	s.process(&models.Update{CallbackQuery: &models.CallbackQuery{
		ID: "q", From: models.User{ID: 1}, Data: data,
		Message: models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeMessage, Message: textMessage(1, "")},
	}})
	return tg.snapshot()[before:]
}

func send(s *Service, tg *fakeTelegram, msg *models.Message) []telegramCall {
	before := len(tg.snapshot())
	s.process(&models.Update{Message: msg})
	return tg.snapshot()[before:]
}

func methods(calls []telegramCall) string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.method)
	}
	return strings.Join(names, ",")
}

func TestSpaceCallbackData(t *testing.T) {
	longMemo := "memos/" + strings.Repeat("m", 36)
	for _, data := range []callbackData{
		{action: actionPlace, memo: longMemo, space: spaceKey("spaces/" + strings.Repeat("s", 36))},
		{action: actionPlace, memo: longMemo, space: noSpaceKey},
		{action: actionDefault, space: teamKey},
		{action: actionDefault, space: noSpaceKey},
		{action: actionSpace, memo: longMemo},
		{action: actionBack, memo: longMemo},
	} {
		encoded := data.String()
		if len(encoded) > 64 {
			t.Errorf("%q exceeds Telegram's 64 byte limit", encoded)
		}
		if got, err := parseCallbackData(encoded); err != nil || got != data || !got.action.valid() {
			t.Errorf("round trip of %q: %+v, %v", encoded, got, err)
		}
	}
	for _, raw := range []string{"sp memos/abc", "sp memos/abc key extra", "sd", "sd a b"} {
		if _, err := parseCallbackData(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestFindSpace(t *testing.T) {
	spaces := []memosapi.Space{{Name: "spaces/team"}, {Name: "spaces/home"}}
	if space, ok := findSpace(spaces, teamKey); !ok || space.Name != "spaces/team" {
		t.Fatalf("got %+v, %v", space, ok)
	}
	if _, ok := findSpace(spaces, spaceKey("spaces/gone")); ok {
		t.Fatal("resolved a space that is no longer listed")
	}
	if _, ok := findSpace(append(spaces, memosapi.Space{Name: "spaces/team"}), teamKey); ok {
		t.Fatal("resolved an ambiguous key")
	}
}

func TestSpaceButtonOpensPicker(t *testing.T) {
	fake := &spaceMemos{spaces: teamSpaces, memo: memosapi.Memo{Name: "memos/abc", Visibility: memosapi.SPACE, Space: "spaces/team"}}
	s, tg, _ := newSpaceService(t, fake)
	calls := press(s, tg, "space memos/abc")
	if methods(calls) != "editMessageReplyMarkup,answerCallbackQuery" || calls[1].text != "Choose a space" {
		t.Fatalf("unexpected calls: %+v", calls)
	}
	want := "✓ Team_A=sp memos/abc " + teamKey + "|spaces/home=sp memos/abc " + homeKey + "|No space=sp memos/abc -,Back=back memos/abc"
	if got := keyboardLayout(&calls[0].keyboard); got != want {
		t.Fatalf("picker:\n got %s\nwant %s", got, want)
	}
}

func TestSpaceButtonWithoutSpaces(t *testing.T) {
	s, tg, _ := newSpaceService(t, &spaceMemos{spaces: `{}`})
	calls := press(s, tg, "space memos/abc")
	if methods(calls) != "answerCallbackQuery" || calls[0].text != noSpaces || !calls[0].alert {
		t.Fatalf("unexpected calls: %+v", calls)
	}
}

func TestPlaceMemoInSpace(t *testing.T) {
	fake := &spaceMemos{spaces: teamSpaces}
	s, tg, _ := newSpaceService(t, fake)
	calls := press(s, tg, "sp memos/abc "+teamKey)
	if len(fake.patches) != 1 || fake.patches[0].mask != "space,visibility" || fake.patches[0].body.Space != "spaces/team" || fake.patches[0].body.Visibility != memosapi.SPACE {
		t.Fatalf("unexpected update: %+v", fake.patches)
	}
	if methods(calls) != "editMessageText,answerCallbackQuery" || calls[1].text != "Memo updated" {
		t.Fatalf("unexpected calls: %+v", calls)
	}
	// The title is user text, so its Markdown characters are escaped.
	if want := fmt.Sprintf(`Memo updated as SPACE in Team\_A with [memos/abc](%s/memos/abc)`, s.memos.BaseURL()); calls[0].text != want {
		t.Fatalf("confirmation:\n got %q\nwant %q", calls[0].text, want)
	}
	if !strings.Contains(keyboardLayout(&calls[0].keyboard), "Space=space memos/abc") {
		t.Fatalf("standard keyboard was not restored: %s", keyboardLayout(&calls[0].keyboard))
	}
}

func TestRemoveMemoFromSpace(t *testing.T) {
	for _, tc := range []struct {
		visibility, want memosapi.MemoVisibility
	}{
		{memosapi.SPACE, memosapi.PRIVATE}, // SPACE requires a space
		{memosapi.PUBLIC, memosapi.PUBLIC},
	} {
		t.Run(string(tc.visibility), func(t *testing.T) {
			fake := &spaceMemos{spaces: teamSpaces, memo: memosapi.Memo{Name: "memos/abc", Visibility: tc.visibility, Space: "spaces/team"}}
			s, tg, _ := newSpaceService(t, fake)
			calls := press(s, tg, "sp memos/abc -")
			if len(fake.patches) != 1 || fake.patches[0].mask != "space,visibility" || fake.patches[0].body.Space != "" || fake.patches[0].body.Visibility != tc.want {
				t.Fatalf("unexpected update: %+v", fake.patches)
			}
			if fake.spaceLists != 0 {
				t.Fatal("removing a memo from its space should not list spaces")
			}
			if methods(calls) != "editMessageText,answerCallbackQuery" || !strings.HasPrefix(calls[0].text, "Memo updated as "+string(tc.want)+" with ") {
				t.Fatalf("unexpected calls: %+v", calls)
			}
		})
	}
}

func TestPlaceMemoWithStaleKey(t *testing.T) {
	fake := &spaceMemos{spaces: teamSpaces}
	s, tg, _ := newSpaceService(t, fake)
	calls := press(s, tg, "sp memos/abc "+spaceKey("spaces/gone"))
	if methods(calls) != "answerCallbackQuery" || calls[0].text != "Spaces changed, tap Space again" || len(fake.patches) != 0 {
		t.Fatalf("unexpected calls: %+v, patches %+v", calls, fake.patches)
	}
}

func TestBackRestoresKeyboard(t *testing.T) {
	fake := &spaceMemos{spaces: teamSpaces}
	s, tg, _ := newSpaceService(t, fake)
	calls := press(s, tg, "back memos/abc")
	if methods(calls) != "editMessageReplyMarkup,answerCallbackQuery" {
		t.Fatalf("unexpected calls: %+v", calls)
	}
	if want := keyboardLayout(s.memoKeyboard(&memosapi.Memo{Name: "memos/abc"})); keyboardLayout(&calls[0].keyboard) != want {
		t.Fatalf("got %s", keyboardLayout(&calls[0].keyboard))
	}
	if len(fake.patches) != 0 {
		t.Fatal("Back changed the memo")
	}
	if calls := press(s, tg, "back users/abc"); calls[0].text != "Invalid command" {
		t.Fatalf("unexpected calls: %+v", calls)
	}
}

func TestServerWithoutSpaces(t *testing.T) {
	t.Run("detected at runtime", func(t *testing.T) {
		fake := &spaceMemos{}
		s, tg, _ := newSpaceService(t, fake)
		calls := press(s, tg, "space memos/abc")
		if methods(calls) != "answerCallbackQuery" || calls[0].text != spacesUnsupported {
			t.Fatalf("unexpected calls: %+v", calls)
		}
		if s.spaces.Load() {
			t.Fatal("unsupported server was not remembered")
		}
		// Later presses fail fast, and new keyboards drop the Space button.
		press(s, tg, "space memos/abc")
		if fake.spaceLists != 1 || strings.Contains(keyboardLayout(s.memoKeyboard(&fake.memo)), "Space") {
			t.Fatalf("spaces still offered: lists=%d", fake.spaceLists)
		}
	})

	t.Run("from the instance version", func(t *testing.T) {
		fake := &spaceMemos{spaces: teamSpaces}
		s, tg, data := newSpaceService(t, fake)
		if err := data.SetDefaultSpace(1, store.Space{Name: "spaces/team", Title: "Team"}); err != nil {
			t.Fatal(err)
		}
		s.useInstanceProfile(&memosapi.InstanceProfile{Version: "0.30.0"})
		for data, method := range map[string]string{"space memos/abc": "answerCallbackQuery", "sp memos/abc -": "answerCallbackQuery", "sd -": "answerCallbackQuery"} {
			if calls := press(s, tg, data); methods(calls) != method || calls[0].text != spacesUnsupported {
				t.Fatalf("%s: unexpected calls %+v", data, calls)
			}
		}
		if calls := send(s, tg, textMessage(1, "/space")); len(calls) != 1 || calls[0].text != spacesUnsupported {
			t.Fatalf("/space: unexpected calls %+v", calls)
		}
		// A saved default is ignored rather than sent to a server that would
		// reject it.
		calls := send(s, tg, textMessage(1, "note"))
		if len(fake.created) != 1 || fake.created[0].Space != "" || !strings.Contains(calls[0].text, "Content saved as PRIVATE") {
			t.Fatalf("created %+v, replies %+v", fake.created, calls)
		}
		if strings.Contains(keyboardLayout(&calls[0].keyboard), "Space") || fake.spaceLists != 0 {
			t.Fatalf("spaces offered on 0.30: %s", keyboardLayout(&calls[0].keyboard))
		}
	})
}

func TestSpaceCommand(t *testing.T) {
	t.Run("lists spaces", func(t *testing.T) {
		s, tg, _ := newSpaceService(t, &spaceMemos{spaces: teamSpaces})
		calls := send(s, tg, textMessage(1, "/space"))
		if len(calls) != 1 || calls[0].text != "New memos are private. Choose a default space for them:" {
			t.Fatalf("unexpected calls: %+v", calls)
		}
		want := "Team_A=sd " + teamKey + "|spaces/home=sd " + homeKey + "|No space=sd -"
		if got := keyboardLayout(&calls[0].keyboard); got != want {
			t.Fatalf("picker:\n got %s\nwant %s", got, want)
		}
	})

	t.Run("marks the current default", func(t *testing.T) {
		s, tg, data := newSpaceService(t, &spaceMemos{spaces: teamSpaces})
		if err := data.SetDefaultSpace(1, store.Space{Name: "spaces/home", Title: "Home"}); err != nil {
			t.Fatal(err)
		}
		calls := send(s, tg, textMessage(1, "/space"))
		if calls[0].text != "New memos are saved in Home. Choose another default space:" || !strings.Contains(keyboardLayout(&calls[0].keyboard), "✓ spaces/home=") {
			t.Fatalf("unexpected reply: %+v", calls[0])
		}
	})

	t.Run("no spaces", func(t *testing.T) {
		s, tg, _ := newSpaceService(t, &spaceMemos{spaces: `{}`})
		if calls := send(s, tg, textMessage(1, "/space")); calls[0].text != noSpaces {
			t.Fatalf("unexpected reply: %+v", calls)
		}
	})

	t.Run("unbound", func(t *testing.T) {
		s, tg, _ := newSpaceService(t, &spaceMemos{spaces: teamSpaces})
		if calls := send(s, tg, textMessage(2, "/space")); calls[0].text != startFirst {
			t.Fatalf("unexpected reply: %+v", calls)
		}
	})
}

func TestChooseDefaultSpace(t *testing.T) {
	s, tg, data := newSpaceService(t, &spaceMemos{spaces: teamSpaces})
	calls := press(s, tg, "sd "+teamKey)
	if methods(calls) != "editMessageText,answerCallbackQuery" || calls[0].text != "New memos will be saved in Team_A." || len(calls[0].keyboard.InlineKeyboard) != 0 {
		t.Fatalf("unexpected calls: %+v", calls)
	}
	if got := data.DefaultSpace(1); got != (store.Space{Name: "spaces/team", Title: "Team_A"}) {
		t.Fatalf("saved %+v", got)
	}

	calls = press(s, tg, "sd "+spaceKey("spaces/gone"))
	if methods(calls) != "answerCallbackQuery" || calls[0].text != "Spaces changed, send /space again" || data.DefaultSpace(1).Name != "spaces/team" {
		t.Fatalf("stale choice: %+v", calls)
	}

	calls = press(s, tg, "sd -")
	if calls[0].text != "New memos are private." || data.DefaultSpace(1) != (store.Space{}) {
		t.Fatalf("clearing: %+v, %+v", calls, data.DefaultSpace(1))
	}
}

type failingSpaceStore struct{ UserStore }

func (failingSpaceStore) SetDefaultSpace(int64, store.Space) error { return errors.New("disk full") }

func TestChooseDefaultSpaceReportsSaveFailure(t *testing.T) {
	fake := &spaceMemos{spaces: teamSpaces, t: t}
	tg := newFakeTelegram(t, nil)
	data := openStore(t)
	bindUser(t, data, 1, "token")
	s := newTestService(t, tg, Options{Memos: fake.client(), Store: failingSpaceStore{data}})
	calls := press(s, tg, "sd "+teamKey)
	if methods(calls) != "answerCallbackQuery" || calls[0].text != "Failed to save default space" {
		t.Fatalf("unexpected calls: %+v", calls)
	}
}

func TestMemoCreatedInDefaultSpace(t *testing.T) {
	fake := &spaceMemos{spaces: teamSpaces}
	s, tg, data := newSpaceService(t, fake)
	if err := data.SetDefaultSpace(1, store.Space{Name: "spaces/team", Title: "Team"}); err != nil {
		t.Fatal(err)
	}
	calls := send(s, tg, textMessage(1, "note"))
	if len(fake.created) != 1 || fake.created[0].Space != "spaces/team" || fake.created[0].Visibility != memosapi.SPACE {
		t.Fatalf("created %+v", fake.created)
	}
	if len(calls) != 1 || !strings.HasPrefix(calls[0].text, "Content saved as SPACE in Team with [memos/new1]") {
		t.Fatalf("unexpected reply: %+v", calls)
	}
}

func TestUnavailableDefaultSpace(t *testing.T) {
	rejectSpace := func(status int) func(memosapi.Memo) int {
		return func(memo memosapi.Memo) int {
			if memo.Space != "" {
				return status
			}
			return 0
		}
	}
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusBadRequest} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			fake := &spaceMemos{spaces: teamSpaces, createStatus: rejectSpace(status)}
			s, tg, data := newSpaceService(t, fake)
			if err := data.SetDefaultSpace(1, store.Space{Name: "spaces/gone", Title: "Old_team"}); err != nil {
				t.Fatal(err)
			}
			calls := send(s, tg, textMessage(1, "note"))
			if len(fake.created) != 2 || fake.created[1].Space != "" || fake.created[1].Visibility != memosapi.PRIVATE {
				t.Fatalf("expected one retry without the space: %+v", fake.created)
			}
			if len(calls) != 1 || !strings.HasPrefix(calls[0].text, "Content saved as PRIVATE with [memos/new2]") ||
				!strings.HasSuffix(calls[0].text, `Your default space Old\_team is no longer available, so this memo was saved without it. Use /space to choose another.`) {
				t.Fatalf("unexpected reply: %q", calls[0].text)
			}
			if data.DefaultSpace(1) != (store.Space{}) {
				t.Fatal("unavailable default space was kept")
			}
		})
	}

	t.Run("content rejected too", func(t *testing.T) {
		fake := &spaceMemos{spaces: teamSpaces, createStatus: func(memosapi.Memo) int { return http.StatusBadRequest }}
		s, tg, data := newSpaceService(t, fake)
		team := store.Space{Name: "spaces/team", Title: "Team"}
		if err := data.SetDefaultSpace(1, team); err != nil {
			t.Fatal(err)
		}
		if calls := send(s, tg, textMessage(1, "note")); calls[0].text != "Failed to create memo" {
			t.Fatalf("unexpected reply: %+v", calls)
		}
		if data.DefaultSpace(1) != team {
			t.Fatal("default space was cleared although the server rejected the content")
		}
	})

	t.Run("server error", func(t *testing.T) {
		fake := &spaceMemos{spaces: teamSpaces, createStatus: rejectSpace(http.StatusInternalServerError)}
		s, tg, data := newSpaceService(t, fake)
		if err := data.SetDefaultSpace(1, store.Space{Name: "spaces/team"}); err != nil {
			t.Fatal(err)
		}
		if calls := send(s, tg, textMessage(1, "note")); calls[0].text != "Failed to create memo" || len(fake.created) != 1 {
			t.Fatalf("server errors must not be retried without the space: %+v, %d", calls, len(fake.created))
		}
	})

	t.Run("album reports once", func(t *testing.T) {
		fake := &spaceMemos{spaces: teamSpaces, createStatus: rejectSpace(http.StatusNotFound)}
		s, tg, data := newSpaceService(t, fake)
		if err := data.SetDefaultSpace(1, store.Space{Name: "spaces/gone", Title: "Old"}); err != nil {
			t.Fatal(err)
		}
		part := func(caption string) *models.Message {
			msg := textMessage(1, "")
			msg.Caption, msg.MediaGroupID = caption, "album"
			return msg
		}
		first := send(s, tg, part("album"))
		second := send(s, tg, part(""))
		if !strings.Contains(first[0].text, "no longer available") || strings.Contains(second[0].text, "no longer available") || len(fake.created) != 2 {
			t.Fatalf("first %q, second %q, created %d", first[0].text, second[0].text, len(fake.created))
		}
	})
}
