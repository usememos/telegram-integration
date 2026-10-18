package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memos"
	"github.com/usememos/memogram/internal/store"
)

// telegramCall is a request the bot made to the fake Telegram API.
type telegramCall struct {
	method, text string
	chatID       int64
	alert        bool
	keyboard     models.InlineKeyboardMarkup
}

// fakeFile is a file the fake Telegram API serves.
type fakeFile struct {
	path, contentType string
	content           []byte
	reportedSize      int // overrides the size getFile reports, when non-zero
	status            int // download status, when non-zero
}

// fakeTelegram implements the Bot API methods Memogram uses and records each
// call. File IDs missing from files fail getFile.
type fakeTelegram struct {
	t     *testing.T
	files map[string]fakeFile

	mu        sync.Mutex
	calls     []telegramCall
	downloads int
}

func newFakeTelegram(t *testing.T, files map[string]fakeFile) *fakeTelegram {
	return &fakeTelegram{t: t, files: files}
}

func (tg *fakeTelegram) snapshot() []telegramCall {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	return append([]telegramCall(nil), tg.calls...)
}

func (tg *fakeTelegram) downloadCount() int {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	return tg.downloads
}

func (tg *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t := tg.t
	if strings.HasPrefix(r.URL.Path, "/file/") {
		tg.mu.Lock()
		tg.downloads++
		tg.mu.Unlock()
		for _, file := range tg.files {
			if strings.HasSuffix(r.URL.Path, "/"+file.path) {
				if file.status != 0 {
					w.WriteHeader(file.status)
					return
				}
				w.Header().Set("Content-Type", file.contentType)
				_, _ = w.Write(file.content)
				return
			}
		}
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		if r.MultipartForm != nil {
			defer func() { _ = r.MultipartForm.RemoveAll() }()
		}
	} else if err := r.ParseForm(); err != nil {
		t.Error(err)
	}
	method := filepath.Base(r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	switch method {
	case "getFile":
		id := r.FormValue("file_id")
		file, ok := tg.files[id]
		if !ok {
			fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"file unavailable"}`)
			return
		}
		size := len(file.content)
		if file.reportedSize != 0 {
			size = file.reportedSize
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"file_id": id, "file_path": file.path, "file_size": size}})
	case "sendMessage", "editMessageText", "editMessageReplyMarkup", "answerCallbackQuery":
		chatID, _ := strconv.ParseInt(r.FormValue("chat_id"), 10, 64)
		var keyboard models.InlineKeyboardMarkup
		if markup := r.FormValue("reply_markup"); markup != "" {
			if err := json.Unmarshal([]byte(markup), &keyboard); err != nil {
				t.Errorf("invalid keyboard: %v", err)
			}
		}
		tg.mu.Lock()
		tg.calls = append(tg.calls, telegramCall{method: method, text: r.FormValue("text"), chatID: chatID, alert: r.FormValue("show_alert") == "true", keyboard: keyboard})
		tg.mu.Unlock()
		if method == "answerCallbackQuery" {
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		} else {
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":2}}`)
		}
	default:
		t.Errorf("unexpected Telegram method %s", method)
		w.WriteHeader(http.StatusNotFound)
	}
}

// newTestService builds a Service against a fake Telegram API, the same way
// production does, with updates handled synchronously. Unset Memos and Store
// options default to an unreachable server and a temporary store.
func newTestService(t *testing.T, telegram http.Handler, opts Options) *Service {
	t.Helper()
	server := httptest.NewServer(telegram)
	t.Cleanup(server.Close)
	opts.Token = "123:test-token"
	opts.ServerURL = server.URL
	opts.BotOptions = append(opts.BotOptions, bot.WithSkipGetMe(), bot.WithNotAsyncHandlers())
	if opts.Memos == nil {
		opts.Memos = memos.NewClient("http://127.0.0.1:1")
	}
	if opts.Store == nil {
		opts.Store = openStore(t)
	}
	if opts.Downloads == nil {
		opts.Downloads = &http.Client{Timeout: 10 * time.Second}
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	data, err := store.Open(filepath.Join(t.TempDir(), "data.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func bindUser(t *testing.T, data UserStore, userID int64, token string) {
	t.Helper()
	if err := data.SetAccessToken(userID, token); err != nil {
		t.Fatal(err)
	}
}

func (s *Service) process(update *models.Update) {
	s.bot.ProcessUpdate(context.Background(), update)
}

func textMessage(userID int64, text string) *models.Message {
	return &models.Message{ID: 1, Chat: models.Chat{ID: userID, Type: models.ChatTypePrivate}, From: &models.User{ID: userID}, Text: text}
}

// fakeMemos serves the Memos endpoints a unit test needs, keyed by
// "METHOD /path"; anything else fails the test.
func fakeMemos(t *testing.T, routes map[string]http.HandlerFunc) *memos.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if route, ok := routes[r.Method+" "+r.URL.Path]; ok {
			route(w, r)
			return
		}
		t.Errorf("unexpected Memos request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	return memos.NewClient(server.URL)
}

func respond(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }
}

// keyboardLayout renders a keyboard as "text=data" buttons, with "," between
// buttons and "|" between rows.
func keyboardLayout(keyboard *models.InlineKeyboardMarkup) string {
	rows := make([]string, 0, len(keyboard.InlineKeyboard))
	for _, row := range keyboard.InlineKeyboard {
		buttons := make([]string, 0, len(row))
		for _, button := range row {
			buttons = append(buttons, button.Text+"="+button.CallbackData)
		}
		rows = append(rows, strings.Join(buttons, ","))
	}
	return strings.Join(rows, "|")
}
