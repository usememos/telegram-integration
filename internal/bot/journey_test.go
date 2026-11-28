package bot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memos"
	"github.com/usememos/memogram/internal/memosapi"
	"github.com/usememos/memogram/internal/store"
)

// The journey tests follow a user through several steps against a stateful
// fake Memos, pressing the buttons the bot actually offered. They run in an
// ordinary `go test`; TestMemosIntegration repeats the same kind of steps
// against a real server.

const (
	alice int64 = 1
	bob   int64 = 2

	aliceToken = "alice-token"
	bobToken   = "bob-token"
)

var searchFilter = regexp.MustCompile(`^content\.contains\((".*")\) && creator == (".*")$`)

// memosServer is an in-memory Memos: users identified by access token, their
// memos and attachments, and the spaces they are members of.
type memosServer struct {
	t   *testing.T
	url string
	// instanceURL is the public address the instance profile reports.
	instanceURL string

	mu          sync.Mutex
	users       map[string]memosapi.User    // by access token
	spaces      map[string][]memosapi.Space // by user name
	memos       []*memosapi.Memo
	attachments []memosapi.Attachment
}

// newMemosServer serves Alice, a member of two spaces, and Bob, a member of
// none.
func newMemosServer(t *testing.T) *memosServer {
	t.Helper()
	m := &memosServer{
		t: t,
		users: map[string]memosapi.User{
			aliceToken: {Name: "users/alice", Username: "alice", DisplayName: "Alice"},
			bobToken:   {Name: "users/bob", Username: "bob", DisplayName: "Bob"},
		},
		spaces: map[string][]memosapi.Space{
			"users/alice": {{Name: "spaces/team", Title: "Team_A"}, {Name: "spaces/home"}},
		},
	}
	server := httptest.NewServer(m)
	t.Cleanup(server.Close)
	m.url = server.URL
	return m
}

func (m *memosServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	fail := func(status int, message string) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
	}
	if r.URL.Path == "/api/instance/profile" {
		_ = json.NewEncoder(w).Encode(memosapi.InstanceProfile{Version: "26.11.3", InstanceUrl: m.instanceURL})
		return
	}
	user, ok := m.users[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
	if !ok {
		fail(http.StatusUnauthorized, "invalid access token")
		return
	}
	isMember := func(space string) bool {
		return slices.ContainsFunc(m.spaces[user.Name], func(s memosapi.Space) bool { return s.Name == space })
	}

	switch route := r.Method + " " + r.URL.Path; {
	case route == "GET /api/auth/me":
		_ = json.NewEncoder(w).Encode(memosapi.GetCurrentUserResponse{User: user})
	case route == "GET /api/spaces":
		_ = json.NewEncoder(w).Encode(memosapi.ListSpacesResponse{Spaces: m.spaces[user.Name]})
	case route == "POST /api/memos":
		var memo memosapi.Memo
		if err := json.NewDecoder(r.Body).Decode(&memo); err != nil {
			m.t.Error(err)
		}
		if memo.Space != "" && !isMember(memo.Space) {
			fail(http.StatusNotFound, "space not found")
			return
		}
		memo.Name = fmt.Sprintf("memos/%d", len(m.memos)+1)
		memo.Creator = user.Name
		m.memos = append(m.memos, &memo)
		_ = json.NewEncoder(w).Encode(memo)
	case route == "GET /api/memos":
		match := searchFilter.FindStringSubmatch(r.URL.Query().Get("filter"))
		if match == nil {
			m.t.Errorf("unexpected filter %q", r.URL.Query().Get("filter"))
			fail(http.StatusBadRequest, "invalid filter")
			return
		}
		text, _ := strconv.Unquote(match[1])
		creator, _ := strconv.Unquote(match[2])
		found := []memosapi.Memo{}
		for _, memo := range m.memos {
			if memo.Creator == creator && strings.Contains(memo.Content, text) {
				found = append(found, *memo)
			}
		}
		_ = json.NewEncoder(w).Encode(memosapi.ListMemosResponse{Memos: found})
	case route == "POST /api/attachments":
		var attachment memosapi.Attachment
		if err := json.NewDecoder(r.Body).Decode(&attachment); err != nil {
			m.t.Error(err)
		}
		attachment.Name = fmt.Sprintf("attachments/%d", len(m.attachments)+1)
		m.attachments = append(m.attachments, attachment)
		_ = json.NewEncoder(w).Encode(attachment)
	case strings.HasPrefix(r.URL.Path, "/api/memos/"):
		name := strings.TrimPrefix(r.URL.Path, "/api/")
		i := slices.IndexFunc(m.memos, func(memo *memosapi.Memo) bool { return memo.Name == name })
		// Like Memos, hide other users' private memos rather than refuse them.
		if i < 0 || (m.memos[i].Creator != user.Name && m.memos[i].Visibility == memosapi.PRIVATE) {
			fail(http.StatusNotFound, "memo not found")
			return
		}
		memo := m.memos[i]
		if r.Method == http.MethodPatch {
			if memo.Creator != user.Name {
				fail(http.StatusForbidden, "permission denied")
				return
			}
			var body memosapi.Memo
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				m.t.Error(err)
			}
			for field := range strings.SplitSeq(r.URL.Query().Get("updateMask"), ",") {
				switch field {
				case memos.FieldContent:
					memo.Content = body.Content
				case memos.FieldVisibility:
					memo.Visibility = body.Visibility
				case memos.FieldPinned:
					memo.Pinned = body.Pinned
				case memos.FieldSpace:
					memo.Space = body.Space
				default:
					m.t.Errorf("unexpected update mask field %q", field)
				}
			}
		}
		_ = json.NewEncoder(w).Encode(memo)
	default:
		m.t.Errorf("unexpected Memos request %s", route)
		fail(http.StatusNotFound, "not found")
	}
}

// memo returns the stored memo, failing the test if it does not exist.
func (m *memosServer) memo(name string) memosapi.Memo {
	m.t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, memo := range m.memos {
		if memo.Name == name {
			return *memo
		}
	}
	m.t.Fatalf("no memo %s", name)
	return memosapi.Memo{}
}

func (m *memosServer) memoCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.memos)
}

// files returns the names and decoded contents of a memo's attachments.
func (m *memosServer) files(memo string) map[string]string {
	m.t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	files := map[string]string{}
	for _, attachment := range m.attachments {
		if attachment.Memo != memo {
			continue
		}
		content, err := base64.StdEncoding.DecodeString(attachment.Content)
		if err != nil {
			m.t.Fatal(err)
		}
		files[attachment.Filename] = string(content)
	}
	return files
}

// leave removes a user from a space.
func (m *memosServer) leave(user, space string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spaces[user] = slices.DeleteFunc(m.spaces[user], func(s memosapi.Space) bool { return s.Name == space })
}

// journey drives the bot the way Telegram users do.
type journey struct {
	t     *testing.T
	s     *Service
	tg    *fakeTelegram
	memos *memosServer
}

// newJourney starts a bot with an empty store at dataPath, or reopens the
// store already there.
func newJourney(t *testing.T, m *memosServer, dataPath string) *journey {
	t.Helper()
	data, err := store.Open(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	tg := newFakeTelegram(t, map[string]fakeFile{
		"photo":    {path: "photos/image.png", contentType: "image/png", content: []byte("photo bytes")},
		"document": {path: "documents/file_7.txt", contentType: "text/plain", content: []byte("document bytes")},
	})
	return &journey{t: t, s: newTestService(t, tg, Options{Memos: memos.NewClient(m.url), Store: data}), tg: tg, memos: m}
}

// send delivers a message and returns the bot's single reply, which must
// contain want.
func (j *journey) send(msg *models.Message, want string) telegramCall {
	j.t.Helper()
	before := len(j.tg.snapshot())
	j.s.process(&models.Update{Message: msg})
	calls := j.tg.snapshot()[before:]
	if len(calls) != 1 || calls[0].method != "sendMessage" || calls[0].chatID != msg.Chat.ID || !strings.Contains(calls[0].text, want) {
		j.t.Fatalf("want one reply containing %q, got %+v", want, calls)
	}
	return calls[0]
}

func (j *journey) say(user int64, text, want string) telegramCall {
	j.t.Helper()
	return j.send(textMessage(user, text), want)
}

// tap presses a button on a message the bot sent or edited. It returns the
// message as the bot left it, when the bot changed it, and the notification
// the user sees.
func (j *journey) tap(user int64, on telegramCall, button string) (edited, notice telegramCall) {
	j.t.Helper()
	var data string
	for _, row := range on.keyboard.InlineKeyboard {
		for _, b := range row {
			if b.Text == button || b.Text == "✓ "+button {
				data = b.CallbackData
			}
		}
	}
	if data == "" {
		j.t.Fatalf("no %q button in %s", button, keyboardLayout(&on.keyboard))
	}
	before := len(j.tg.snapshot())
	j.s.process(&models.Update{CallbackQuery: &models.CallbackQuery{
		ID: "q", From: models.User{ID: user}, Data: data,
		Message: models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeMessage, Message: textMessage(user, "")},
	}})
	calls := j.tg.snapshot()[before:]
	if len(calls) == 0 || len(calls) > 2 || calls[len(calls)-1].method != "answerCallbackQuery" {
		j.t.Fatalf("tap %q: unexpected calls %+v", button, calls)
	}
	if len(calls) == 2 {
		edited = calls[0]
	}
	return edited, calls[len(calls)-1]
}

// savedName returns the name of the memo a reply links to.
func (j *journey) savedName(reply telegramCall) string {
	j.t.Helper()
	name := regexp.MustCompile(`memos/\d+`).FindString(reply.text)
	if name == "" {
		j.t.Fatalf("reply links no memo: %s", reply.text)
	}
	return name
}

func dataPath(t *testing.T) string {
	return filepath.Join(t.TempDir(), "data.txt")
}

// A new user links their account, saves a note, changes it with the buttons
// under the reply, and finds it again.
func TestJourneyFirstNote(t *testing.T) {
	m := newMemosServer(t)
	j := newJourney(t, m, dataPath(t))

	j.say(alice, "buy milk", startFirst)
	j.say(alice, "/start", "Usage: /start <access_token>")
	j.say(alice, "/start mistyped", "Invalid access token")
	if m.memoCount() != 0 {
		t.Fatal("a memo was saved before the account was linked")
	}
	j.say(alice, "/start "+aliceToken, "Hello Alice!")

	saved := j.say(alice, "buy milk", "Content saved as PRIVATE with [memos/1]("+m.url+"/memos/1)")
	if memo := m.memo("memos/1"); memo.Content != "buy milk" || memo.Creator != "users/alice" || memo.Visibility != memosapi.PRIVATE {
		t.Fatalf("saved %+v", memo)
	}

	edited, notice := j.tap(alice, saved, "Public")
	if !strings.Contains(edited.text, "Memo updated as PUBLIC") || notice.text != "Memo updated" || m.memo("memos/1").Visibility != memosapi.PUBLIC {
		t.Fatalf("Public: %+v, %+v", edited, notice)
	}
	edited, _ = j.tap(alice, edited, "Pin")
	if !strings.HasSuffix(edited.text, "📌") || !m.memo("memos/1").Pinned {
		t.Fatalf("Pin: %+v", edited)
	}
	// Each button changes one field and keeps the others.
	edited, _ = j.tap(alice, edited, "Private")
	if memo := m.memo("memos/1"); !strings.Contains(edited.text, "Memo updated as PRIVATE") || memo.Visibility != memosapi.PRIVATE || !memo.Pinned || memo.Content != "buy milk" {
		t.Fatalf("Private: %+v, memo %+v", edited, memo)
	}
	if _, _ = j.tap(alice, edited, "Pin"); m.memo("memos/1").Pinned {
		t.Fatal("the second Pin did not unpin the memo")
	}

	if found := j.say(alice, "/search milk", "buy milk"); found.text != "memos/1\nbuy milk" {
		t.Fatalf("search result %q", found.text)
	}
	j.say(alice, "/search bread", "No memos found")
}

// Photos, files and albums become memos with the files attached.
func TestJourneyCaptureFiles(t *testing.T) {
	m := newMemosServer(t)
	j := newJourney(t, m, dataPath(t))
	j.say(alice, "/start "+aliceToken, "Hello Alice!")

	photo := textMessage(alice, "")
	photo.Caption = "whiteboard"
	photo.Photo = []models.PhotoSize{{FileID: "thumbnail"}, {FileID: "photo"}}
	name := j.savedName(j.send(photo, "Content saved as PRIVATE"))
	if memo, files := m.memo(name), m.files(name); memo.Content != "whiteboard" || len(files) != 1 || files["image.png"] != "photo bytes" {
		t.Fatalf("photo: memo %+v, files %v", memo, files)
	}

	// A file without a caption is still worth a memo, under the sender's name
	// for it.
	document := textMessage(alice, "")
	document.Document = &models.Document{FileID: "document", FileName: "notes.txt"}
	name = j.savedName(j.send(document, "Content saved"))
	if memo, files := m.memo(name), m.files(name); memo.Content != "" || len(files) != 1 || files["notes.txt"] != "document bytes" {
		t.Fatalf("document: memo %+v, files %v", memo, files)
	}

	// Telegram delivers an album as separate messages; they share one memo.
	before := m.memoCount()
	first := textMessage(alice, "")
	first.MediaGroupID, first.Caption = "trip", "trip photos"
	first.Photo = []models.PhotoSize{{FileID: "photo"}}
	second := textMessage(alice, "")
	second.MediaGroupID = "trip"
	second.Document = &models.Document{FileID: "document", FileName: "route.txt"}
	name = j.savedName(j.send(first, "Content saved"))
	if j.savedName(j.send(second, "Content saved")) != name || m.memoCount() != before+1 {
		t.Fatalf("album saved %d memos", m.memoCount()-before)
	}
	if memo, files := m.memo(name), m.files(name); memo.Content != "trip photos" || len(files) != 2 || files["image.png"] != "photo bytes" || files["route.txt"] != "document bytes" {
		t.Fatalf("album: memo %+v, files %v", memo, files)
	}

	// A file Telegram cannot hand over does not lose the note.
	lost := textMessage(alice, "")
	lost.Caption = "receipt"
	lost.Document = &models.Document{FileID: "expired", FileName: "receipt.pdf"}
	name = j.savedName(j.send(lost, "Memo saved, but the attachment failed to upload."))
	if memo, files := m.memo(name), m.files(name); memo.Content != "receipt" || len(files) != 0 {
		t.Fatalf("lost file: memo %+v, files %v", memo, files)
	}
}

// A user picks a default space, saves into it, moves a memo between spaces
// and out again, and keeps saving after leaving the default space.
func TestJourneySpaces(t *testing.T) {
	m := newMemosServer(t)
	j := newJourney(t, m, dataPath(t))
	j.say(alice, "/start "+aliceToken, "Hello Alice!")

	picker := j.say(alice, "/space", "New memos are private. Choose a default space for them:")
	edited, notice := j.tap(alice, picker, "Team_A")
	if edited.text != "New memos will be saved in Team_A." || len(edited.keyboard.InlineKeyboard) != 0 || notice.text != "Default space updated" {
		t.Fatalf("choose default: %+v, %+v", edited, notice)
	}

	saved := j.say(alice, "standup notes", `Content saved as SPACE in Team\_A`)
	name := j.savedName(saved)
	if memo := m.memo(name); memo.Space != "spaces/team" || memo.Visibility != memosapi.SPACE {
		t.Fatalf("saved %+v", memo)
	}

	// Move it to the other space. The picker marks where the memo is now.
	spaces, notice := j.tap(alice, saved, "Space")
	if notice.text != "Choose a space" || !strings.Contains(keyboardLayout(&spaces.keyboard), "✓ Team_A=") {
		t.Fatalf("picker: %s, %+v", keyboardLayout(&spaces.keyboard), notice)
	}
	edited, _ = j.tap(alice, spaces, "spaces/home")
	if memo := m.memo(name); !strings.Contains(edited.text, "Memo updated as SPACE in spaces/home") || memo.Space != "spaces/home" || memo.Visibility != memosapi.SPACE {
		t.Fatalf("move: %+v, memo %+v", edited, memo)
	}

	// Back closes the picker and changes nothing.
	spaces, _ = j.tap(alice, edited, "Space")
	edited, _ = j.tap(alice, spaces, "Back")
	if memo := m.memo(name); memo.Space != "spaces/home" || keyboardLayout(&edited.keyboard) != keyboardLayout(&saved.keyboard) {
		t.Fatalf("back: %s, memo %+v", keyboardLayout(&edited.keyboard), memo)
	}

	// "No space" takes it out and makes it private again.
	spaces, _ = j.tap(alice, edited, "Space")
	edited, _ = j.tap(alice, spaces, "No space")
	if memo := m.memo(name); !strings.Contains(edited.text, "Memo updated as PRIVATE") || memo.Space != "" || memo.Visibility != memosapi.PRIVATE {
		t.Fatalf("no space: %+v, memo %+v", edited, memo)
	}

	// After Alice leaves her default space, the next note is still saved, she
	// is told once, and the default is cleared.
	m.leave("users/alice", "spaces/team")
	lost := j.say(alice, "after leaving", `Your default space Team\_A is no longer available`)
	if memo := m.memo(j.savedName(lost)); !strings.Contains(lost.text, "Content saved as PRIVATE") || memo.Space != "" || memo.Content != "after leaving" {
		t.Fatalf("lost default: %+v, memo %+v", lost, memo)
	}
	if next := j.say(alice, "later", "Content saved as PRIVATE"); strings.Contains(next.text, "no longer available") {
		t.Fatalf("lost default reported twice: %s", next.text)
	}
	picker = j.say(alice, "/space", "New memos are private.")
	if strings.Contains(keyboardLayout(&picker.keyboard), "Team_A") {
		t.Fatalf("left space still offered: %s", keyboardLayout(&picker.keyboard))
	}
}

// A linked account and its default space outlive a restart, and so do the
// buttons under messages sent before it.
func TestJourneySurvivesRestart(t *testing.T) {
	m := newMemosServer(t)
	path := dataPath(t)
	j := newJourney(t, m, path)
	j.say(alice, "/start "+aliceToken, "Hello Alice!")
	j.tap(alice, j.say(alice, "/space", "Choose a default space"), "Team_A")
	before := j.say(alice, "before restart", `Content saved as SPACE in Team\_A`)

	j = newJourney(t, m, path)
	after := j.say(alice, "after restart", `Content saved as SPACE in Team\_A`)
	if memo := m.memo(j.savedName(after)); memo.Creator != "users/alice" || memo.Space != "spaces/team" {
		t.Fatalf("saved %+v", memo)
	}
	if _, notice := j.tap(alice, before, "Pin"); notice.text != "Memo updated" || !m.memo(j.savedName(before)).Pinned {
		t.Fatalf("button from before the restart: %+v", notice)
	}
	j.say(bob, "hello", startFirst)
}

// Two people share one bot. Each sees and changes only their own memos, even
// when pressing a button under the other's message.
func TestJourneyTwoUsers(t *testing.T) {
	m := newMemosServer(t)
	j := newJourney(t, m, dataPath(t))
	j.say(alice, "/start "+aliceToken, "Hello Alice!")
	j.say(bob, "/start "+bobToken, "Hello Bob!")

	aliceSaved := j.say(alice, "shared word, alice", "Content saved")
	bobSaved := j.say(bob, "shared word, bob", "Content saved")
	aliceMemo, bobMemo := j.savedName(aliceSaved), j.savedName(bobSaved)
	if m.memo(aliceMemo).Creator != "users/alice" || m.memo(bobMemo).Creator != "users/bob" {
		t.Fatal("memos were saved under the wrong account")
	}
	if found := j.say(bob, "/search shared word", "shared word, bob"); found.text != bobMemo+"\nshared word, bob" {
		t.Fatalf("Bob's search: %q", found.text)
	}

	// Alice's private memo does not exist as far as Bob can tell.
	if edited, notice := j.tap(bob, aliceSaved, "Public"); notice.text != "Memo "+aliceMemo+" not found" || !notice.alert || edited.method != "" {
		t.Fatalf("Bob on a private memo: %+v, %+v", edited, notice)
	}
	// Once it is public he can read it, but still not change it.
	j.tap(alice, aliceSaved, "Public")
	if _, notice := j.tap(bob, aliceSaved, "Pin"); notice.text != "Failed to update memo" || !notice.alert {
		t.Fatalf("Bob on a public memo: %+v", notice)
	}
	if memo := m.memo(aliceMemo); memo.Pinned || memo.Visibility != memosapi.PUBLIC || memo.Creator != "users/alice" {
		t.Fatalf("Bob changed Alice's memo: %+v", memo)
	}

	// Bob has no spaces, so there is nothing to choose from.
	j.say(bob, "/space", noSpaces)
	if _, notice := j.tap(bob, bobSaved, "Space"); notice.text != noSpaces || !notice.alert {
		t.Fatalf("Bob's Space button: %+v", notice)
	}
}

// pollingTelegram adds what Service.Start needs to fakeTelegram: it records
// the command menu and hands out queued updates to the long poll.
type pollingTelegram struct {
	*fakeTelegram
	updates  chan string // JSON updates, each delivered once
	commands chan string // the names in each setMyCommands call
}

func (tg *pollingTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch filepath.Base(r.URL.Path) {
	case "setMyCommands":
		var commands []models.BotCommand
		if err := json.Unmarshal([]byte(r.FormValue("commands")), &commands); err != nil {
			tg.t.Errorf("invalid commands: %v", err)
		}
		names := make([]string, 0, len(commands))
		for _, command := range commands {
			names = append(names, command.Command)
		}
		tg.commands <- strings.Join(names, ",")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	case "getUpdates":
		// Hold the long poll open until there is an update or the bot stops.
		// The server only notices the bot hanging up once the request body
		// has been read.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case update := <-tg.updates:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"ok":true,"result":[%s]}`, update)
		case <-r.Context().Done():
		}
	default:
		tg.fakeTelegram.ServeHTTP(w, r)
	}
}

// The operator starts the bot: it reads the instance profile, publishes its
// commands, and saves a message that arrives through polling, linking to the
// instance's public address.
func TestJourneyStartup(t *testing.T) {
	m := newMemosServer(t)
	m.instanceURL = "https://memos.example.com/"
	data := openStore(t)
	bindUser(t, data, alice, aliceToken)
	tg := &pollingTelegram{fakeTelegram: newFakeTelegram(t, nil), updates: make(chan string, 1), commands: make(chan string, 1)}
	s := newTestService(t, tg, Options{Memos: memos.NewClient(m.url), Store: data})

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		s.Start(ctx)
	}()
	// Stop the bot before the fake Telegram server closes, on every path.
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Error("Start did not return after its context was canceled")
		}
	})

	select {
	case commands := <-tg.commands:
		if commands != "start,search,space" {
			t.Fatalf("command menu %q", commands)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the command menu was not published")
	}

	tg.updates <- `{"update_id":1,"message":{"message_id":5,"date":1,"chat":{"id":1,"type":"private"},"from":{"id":1,"is_bot":false,"first_name":"Alice"},"text":"polled note"}}`
	deadline := time.Now().Add(10 * time.Second)
	for len(tg.snapshot()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the polled message got no reply")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reply := tg.snapshot()[0]; reply.chatID != alice || !strings.Contains(reply.text, "Content saved as PRIVATE with [memos/1](https://memos.example.com/memos/1)") {
		t.Fatalf("reply %+v", reply)
	}
	if memo := m.memo("memos/1"); memo.Content != "polled note" || memo.Creator != "users/alice" {
		t.Fatalf("saved %+v", memo)
	}
}
