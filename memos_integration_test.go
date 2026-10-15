package memogram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/usememos/memogram/internal/memosapi"
	"github.com/usememos/memogram/store"
)

type integrationUser struct {
	user          *memosapi.User
	session, auth *MemosClient
	pat           *memosapi.CreatePersonalAccessTokenResponse
	telegramID    int64
}

type telegramCall struct {
	method, text string
	chatID       int64
	keyboard     models.InlineKeyboardMarkup
}
type telegramFile struct {
	path, contentType string
	content           []byte
}

type integrationTelegram struct {
	mu    sync.Mutex
	calls []telegramCall
	files map[string]telegramFile
}

func (tg *integrationTelegram) snapshot() []telegramCall {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	return append([]telegramCall(nil), tg.calls...)
}

func (tg *integrationTelegram) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if strings.HasPrefix(r.URL.Path, "/file/") {
		for _, file := range tg.files {
			if strings.HasSuffix(r.URL.Path, "/"+file.path) {
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
			defer r.MultipartForm.RemoveAll()
		}
	} else if err := r.ParseForm(); err != nil {
		t.Error(err)
	}
	method := filepath.Base(r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	switch method {
	case "getFile":
		id := r.FormValue("file_id")
		if file, ok := tg.files[id]; ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"file_id": id, "file_path": file.path, "file_size": len(file.content)}})
		} else {
			fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"file unavailable"}`)
		}
	case "sendMessage", "editMessageText", "answerCallbackQuery":
		chatID, _ := strconv.ParseInt(r.FormValue("chat_id"), 10, 64)
		var keyboard models.InlineKeyboardMarkup
		if markup := r.FormValue("reply_markup"); markup != "" {
			if err := json.Unmarshal([]byte(markup), &keyboard); err != nil {
				t.Errorf("invalid keyboard: %v", err)
			}
		}
		tg.mu.Lock()
		tg.calls = append(tg.calls, telegramCall{method: method, text: r.FormValue("text"), chatID: chatID, keyboard: keyboard})
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

// MEMOS_TEST_URL must point to a fresh, disposable Memos instance. All scenarios
// run unchanged against every image in CI's compatibility matrix.
func TestMemosIntegration(t *testing.T) {
	baseURL := os.Getenv("MEMOS_TEST_URL")
	if baseURL == "" {
		t.Skip("set MEMOS_TEST_URL to a fresh Memos instance to run API compatibility tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := NewMemosClient(baseURL)
	var profile *memosapi.InstanceProfile
	var err error
	for ctx.Err() == nil {
		profile, err = client.GetInstanceProfile(ctx)
		if err == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Memos did not become ready: %v", err)
	}
	if profile == nil || !profile.NeedsSetup {
		t.Fatal("MEMOS_TEST_URL must point to a fresh disposable instance, with no users")
	}
	t.Logf("testing Memos %s", profile.Version)

	newUser := func(creator *MemosClient, username string, role memosapi.UserRole, id int64) integrationUser {
		t.Helper()
		user, err := decodeAPIResponse[memosapi.User](creator.api.UserServiceCreateUser(ctx, nil, memosapi.User{Username: username, Password: "integration-test-password", Role: role, State: memosapi.UserStateNORMAL, DisplayName: username}))
		if err != nil {
			t.Fatalf("create %s: %v", username, err)
		}
		signedIn, err := decodeAPIResponse[memosapi.SignInResponse](client.api.AuthServiceSignIn(ctx, memosapi.SignInRequest{PasswordCredentials: memosapi.SignInRequestPasswordCredentials{Username: username, Password: "integration-test-password"}}))
		if err != nil {
			t.Fatalf("sign in %s: %v", username, err)
		}
		session := client.NewAuthenticatedClient(signedIn.AccessToken)
		pat, err := decodeAPIResponse[memosapi.CreatePersonalAccessTokenResponse](session.api.UserServiceCreatePersonalAccessToken(ctx, strings.TrimPrefix(user.Name, "users/"), memosapi.CreatePersonalAccessTokenRequest{Parent: user.Name, Description: "Memogram integration test"}))
		if err != nil {
			t.Fatalf("create PAT for %s: %v", username, err)
		}
		if pat.Token == "" || pat.PersonalAccessToken.Name == "" {
			t.Fatal("missing PAT or resource name")
		}
		return integrationUser{user: user, session: session, pat: pat, auth: client.NewAuthenticatedClient(pat.Token), telegramID: id}
	}
	admin := newUser(client, "test-admin", memosapi.ADMIN, 1)
	alice := newUser(admin.auth, "test-alice", memosapi.USER, 42)
	bob := newUser(admin.auth, "test-bob", memosapi.USER, 43)

	var picture bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&picture, img); err != nil {
		t.Fatal(err)
	}
	tg := &integrationTelegram{files: map[string]telegramFile{
		"document": {path: "docs/test.txt", contentType: "text/plain", content: []byte("saved document bytes")},
		"photo":    {path: "photos/image.png", contentType: "image/png", content: picture.Bytes()},
		// Small transport fixtures: the bot stores these bytes; it does not decode media.
		"voice": {path: "voice/note.ogg", contentType: "audio/ogg", content: []byte("OggS\x00voice fixture")},
		"video": {path: "video/clip.mp4", contentType: "video/mp4", content: []byte("\x00\x00\x00\x18ftypmp42video fixture")},
	}}
	b := newTestBot(t, func(w http.ResponseWriter, r *http.Request) { tg.serve(t, w, r) })
	data := store.NewStore(filepath.Join(t.TempDir(), "data.txt"))
	if err := data.Init(); err != nil {
		t.Fatal(err)
	}
	s := &Service{bot: b, client: client, config: &Config{ServerAddr: baseURL}, store: data, httpClient: &http.Client{Timeout: 10 * time.Second}, instanceProfile: profile}
	message := func(u integrationUser, text string) *models.Message {
		return &models.Message{ID: 1, Chat: models.Chat{ID: u.telegramID, Type: models.ChatTypePrivate}, From: &models.User{ID: u.telegramID}, Text: text}
	}
	send := func(t *testing.T, m *models.Message, want string) telegramCall {
		t.Helper()
		before := len(tg.snapshot())
		s.handler(ctx, b, &models.Update{Message: m})
		calls := tg.snapshot()[before:]
		if len(calls) != 1 || calls[0].method != "sendMessage" || calls[0].chatID != m.Chat.ID || !strings.Contains(calls[0].text, want) {
			t.Fatalf("want one reply containing %q to chat %d, got %+v", want, m.Chat.ID, calls)
		}
		return calls[0]
	}
	savedMemo := func(t *testing.T, u integrationUser, reply telegramCall) *memosapi.Memo {
		t.Helper()
		name := regexp.MustCompile(`memos/[A-Za-z0-9_-]+`).FindString(reply.text)
		if name == "" {
			t.Fatalf("reply has no saved memo link: %s", reply.text)
		}
		memo, err := u.auth.GetMemo(ctx, name)
		if err != nil {
			t.Fatalf("read saved memo: %v", err)
		}
		if memo.Creator != u.user.Name {
			t.Fatalf("wrong creator: %s", memo.Creator)
		}
		return memo
	}
	listAttachments := func(t *testing.T, u integrationUser, memo *memosapi.Memo) []memosapi.Attachment {
		t.Helper()
		result, err := decodeAPIResponse[memosapi.ListMemoAttachmentsResponse](u.auth.api.MemoServiceListMemoAttachments(ctx, strings.TrimPrefix(memo.Name, "memos/"), nil))
		if err != nil {
			t.Fatalf("list attachments: %v", err)
		}
		return result.Attachments
	}
	checkFiles := func(t *testing.T, u integrationUser, memo *memosapi.Memo, ids ...string) {
		t.Helper()
		attachments := listAttachments(t, u, memo)
		if len(attachments) != len(ids) {
			t.Fatalf("want %d attachments, got %d", len(ids), len(attachments))
		}
		want := map[string]telegramFile{}
		for _, id := range ids {
			file := tg.files[id]
			want[filepath.Base(file.path)] = file
		}
		for _, attachment := range attachments {
			file, ok := want[attachment.Filename]
			if !ok || attachment.Memo != memo.Name || attachment.Type != file.contentType || attachment.Size != strconv.Itoa(len(file.content)) {
				t.Fatalf("unexpected attachment: %+v", attachment)
			}
			delete(want, attachment.Filename)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/file/"+attachment.Name+"/"+attachment.Filename, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := u.auth.httpClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != http.StatusOK || !bytes.Equal(body, file.content) {
				t.Fatalf("stored file bytes differ: status=%d, err=%v", resp.StatusCode, err)
			}
		}
	}
	requireStatus := func(t *testing.T, err error, want ...int) {
		t.Helper()
		var apiErr *MemosAPIError
		if errors.As(err, &apiErr) {
			for _, code := range want {
				if apiErr.StatusCode == code {
					return
				}
			}
		}
		t.Fatalf("want API status %v, got %v", want, err)
	}

	// Bind in shared setup so individual scenarios also work with -run filters.
	for _, u := range []integrationUser{alice, bob} {
		if u.user.Role != memosapi.USER {
			t.Fatal("business scenarios require ordinary users")
		}
		send(t, message(u, "/start "+u.pat.Token), "Hello "+u.user.DisplayName)
		if token, ok := data.GetUserAccessToken(u.telegramID); !ok || token != u.pat.Token {
			t.Fatal("PAT binding was not saved")
		}
	}

	t.Run("save_text_and_search", func(t *testing.T) {
		content := "integration text 保存中文和 emoji 🌱"
		textMemo := savedMemo(t, alice, send(t, message(alice, content), "Content saved"))
		if textMemo.Content != content || textMemo.Visibility != memosapi.PRIVATE || textMemo.Pinned {
			t.Fatalf("incorrect saved text memo: %+v", textMemo)
		}
		checkFiles(t, alice, textMemo)
		send(t, message(alice, "/search 保存中文"), textMemo.Name+"\n"+content)
	})

	for _, tc := range []struct {
		name, file string
		caption    bool
	}{
		{"document_with_caption", "document", true},
		{"document_without_caption", "document", false},
		{"photo_with_caption", "photo", true},
		{"photo_without_caption", "photo", false},
		{"photo_as_document", "photo", true},
		{"voice", "voice", true},
		{"video", "video", true},
	} {
		t.Run("save_"+tc.name, func(t *testing.T) {
			m := message(alice, "")
			if tc.caption {
				m.Caption = "integration " + tc.name
			}
			switch tc.file {
			case "document":
				m.Document = &models.Document{FileID: tc.file}
			case "photo":
				m.Photo = []models.PhotoSize{{FileID: "thumbnail-must-not-be-downloaded", Width: 1, Height: 1}, {FileID: tc.file, Width: 2, Height: 2}}
				if tc.name == "photo_as_document" {
					m.Photo = nil
					m.Document = &models.Document{FileID: tc.file}
				}
			case "voice":
				m.Voice = &models.Voice{FileID: tc.file}
			case "video":
				m.Video = &models.Video{FileID: tc.file}
			}
			memo := savedMemo(t, alice, send(t, m, "Content saved"))
			if memo.Content != m.Caption || memo.Visibility != memosapi.PRIVATE {
				t.Fatalf("incorrect saved media memo: %+v", memo)
			}
			checkFiles(t, alice, memo, tc.file)
		})
	}

	t.Run("attachment_upload_failure_and_recovery", func(t *testing.T) {
		upstream, err := url.Parse(baseURL)
		if err != nil {
			t.Fatal(err)
		}
		proxy := httputil.NewSingleHostReverseProxy(upstream)
		var failUploads atomic.Bool
		var failedUploads atomic.Int32
		failUploads.Store(true)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if failUploads.Load() && r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/attachments") {
				failedUploads.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"message":"storage temporarily unavailable"}`)
				return
			}
			proxy.ServeHTTP(w, r)
		}))
		defer server.Close()
		proxied := NewMemosClient(server.URL)
		if _, err := proxied.GetInstanceProfile(ctx); err != nil {
			t.Fatal(err)
		}
		s.client = proxied
		defer func() { s.client = client }()
		m := message(alice, "")
		m.Caption = "integration upload failure"
		m.Photo = []models.PhotoSize{{FileID: "photo"}}
		reply := send(t, m, "Memo saved, but the attachment failed")
		if strings.Contains(reply.text, "Content saved") || failedUploads.Load() != 1 {
			t.Fatal("failed upload was retried or reported as success")
		}
		memo := savedMemo(t, alice, reply)
		if memo.Content != m.Caption {
			t.Fatal("upload failure lost the memo content")
		}
		checkFiles(t, alice, memo)
		failUploads.Store(false)
		m.Caption = "integration upload recovered"
		memo = savedMemo(t, alice, send(t, m, "Content saved"))
		checkFiles(t, alice, memo, "photo")
	})

	t.Run("album", func(t *testing.T) {
		first := message(alice, "")
		first.Caption = "integration album"
		first.MediaGroupID = "album"
		first.Photo = []models.PhotoSize{{FileID: "photo"}}
		memo := savedMemo(t, alice, send(t, first, "Content saved"))
		second := message(alice, "")
		second.MediaGroupID = "album"
		second.Document = &models.Document{FileID: "document"}
		secondMemo := savedMemo(t, alice, send(t, second, "Content saved"))
		if secondMemo.Name != memo.Name || secondMemo.Content != first.Caption {
			t.Fatal("album did not preserve the first caption and reuse its memo")
		}
		third := message(alice, "")
		third.MediaGroupID = "album"
		third.Photo = []models.PhotoSize{{FileID: "unavailable"}}
		failedMemo := savedMemo(t, alice, send(t, third, "Memo saved, but the attachment failed"))
		if failedMemo.Name != memo.Name {
			t.Fatal("failed album item created a duplicate memo")
		}
		checkFiles(t, alice, memo, "photo", "document")
		found, err := alice.auth.ListMemos(ctx, buildMemoSearchFilter(first.Caption, alice.user))
		if err != nil || len(found) != 1 {
			t.Fatalf("album created duplicate memos: count=%d err=%v", len(found), err)
		}
	})

	t.Run("concurrent_album", func(t *testing.T) {
		before := len(tg.snapshot())
		var wg sync.WaitGroup
		for _, id := range []string{"photo", "document"} {
			m := message(bob, "")
			m.Caption = "integration concurrent album"
			m.MediaGroupID = "concurrent-album"
			m.Document = &models.Document{FileID: id}
			wg.Go(func() { s.handler(ctx, b, &models.Update{Message: m}) })
		}
		wg.Wait()
		calls := tg.snapshot()[before:]
		if len(calls) != 2 {
			t.Fatalf("expected two album confirmations, got %+v", calls)
		}
		for _, call := range calls {
			if call.method != "sendMessage" || call.chatID != bob.telegramID || !strings.Contains(call.text, "Content saved") {
				t.Fatalf("unexpected album reply: %+v", call)
			}
		}
		first := savedMemo(t, bob, calls[0])
		second := savedMemo(t, bob, calls[1])
		if first.Name != second.Name {
			t.Fatal("concurrent album created two memos")
		}
		checkFiles(t, bob, first, "photo", "document")
		found, err := bob.auth.ListMemos(ctx, buildMemoSearchFilter("integration concurrent album", bob.user))
		if err != nil || len(found) != 1 {
			t.Fatalf("concurrent album count=%d err=%v", len(found), err)
		}
	})

	callback := func(t *testing.T, u integrationUser, action, name, want string, success bool) {
		t.Helper()
		// Use the callback data emitted by the actual keyboard for visible actions.
		callbackData := action + " " + name
		if success && action != "protected" {
			found := false
			for _, call := range tg.snapshot() {
				for _, row := range call.keyboard.InlineKeyboard {
					for _, button := range row {
						if button.CallbackData == callbackData {
							callbackData = button.CallbackData
							found = true
						}
					}
				}
			}
			if !found {
				t.Fatalf("keyboard is missing %s button for %s", action, name)
			}
		}
		before := len(tg.snapshot())
		s.callbackQueryHandler(ctx, b, &models.Update{CallbackQuery: &models.CallbackQuery{ID: "callback", From: models.User{ID: u.telegramID}, Data: callbackData, Message: models.MaybeInaccessibleMessage{Type: models.MaybeInaccessibleMessageTypeMessage, Message: message(u, "")}}})
		calls := tg.snapshot()[before:]
		if success {
			if len(calls) != 2 || calls[0].method != "editMessageText" || calls[0].chatID != u.telegramID || !strings.Contains(calls[0].text, name) || calls[1].method != "answerCallbackQuery" || calls[1].text != want {
				t.Fatalf("unexpected callback success: %+v", calls)
			}
		} else if len(calls) != 1 || calls[0].method != "answerCallbackQuery" || !strings.Contains(calls[0].text, want) {
			t.Fatalf("unexpected callback failure: %+v", calls)
		}
	}
	t.Run("visibility_and_pin_buttons", func(t *testing.T) {
		textMemo := savedMemo(t, alice, send(t, message(alice, "integration button content"), "Content saved"))
		for _, step := range []struct {
			action     string
			visibility memosapi.MemoVisibility
			pinned     bool
		}{
			{"public", memosapi.PUBLIC, false}, {"pin", memosapi.PUBLIC, true}, {"protected", memosapi.PROTECTED, true}, {"private", memosapi.PRIVATE, true}, {"pin", memosapi.PRIVATE, false},
		} {
			t.Run(step.action+"_"+string(step.visibility), func(t *testing.T) {
				callback(t, alice, step.action, textMemo.Name, "Memo updated", true)
				memo, err := alice.auth.GetMemo(ctx, textMemo.Name)
				if err != nil {
					t.Fatal(err)
				}
				if memo.Visibility != step.visibility || memo.Pinned != step.pinned || memo.Content != textMemo.Content || memo.Creator != alice.user.Name || memo.State != memosapi.MemoStateNORMAL {
					t.Fatalf("button changed unexpected state: %+v", memo)
				}
			})
		}
	})

	t.Run("user_isolation_and_permission_denial", func(t *testing.T) {
		a := savedMemo(t, alice, send(t, message(alice, "integration shared needle alice"), "Content saved"))
		other := savedMemo(t, bob, send(t, message(bob, "integration shared needle bob"), "Content saved"))
		// Make Bob's memo visible to Alice so this checks the creator filter as well
		// as the server's handling of private memos.
		callback(t, bob, "public", other.Name, "Memo updated", true)
		send(t, message(alice, "/search integration shared needle"), a.Name+"\n"+a.Content)
		send(t, message(bob, "/search integration shared needle"), other.Name+"\n"+other.Content)
		_, err := bob.auth.GetMemo(ctx, a.Name)
		requireStatus(t, err, 403, 404)
		modified := *a
		modified.Pinned = true
		_, err = bob.auth.UpdateMemo(ctx, &modified)
		requireStatus(t, err, 403, 404)
		callback(t, bob, "public", a.Name, "not found", false)
		_, err = alice.auth.GetMemo(ctx, other.Name)
		if err != nil {
			t.Fatalf("public memo should be readable: %v", err)
		}
		callback(t, alice, "pin", other.Name, "Failed to update memo", false)
		for _, item := range []struct {
			u          integrationUser
			memo       *memosapi.Memo
			visibility memosapi.MemoVisibility
		}{{alice, a, memosapi.PRIVATE}, {bob, other, memosapi.PUBLIC}} {
			got, err := item.u.auth.GetMemo(ctx, item.memo.Name)
			if err != nil {
				t.Fatal(err)
			}
			if got.Content != item.memo.Content || got.Visibility != item.visibility || got.Pinned {
				t.Fatalf("unauthorized operation modified memo: %+v", got)
			}
		}
	})

	t.Run("invalid_and_revoked_tokens", func(t *testing.T) {
		invalid := alice
		invalid.telegramID = 99
		send(t, message(invalid, "/start invalid-token"), "Invalid access token")
		if _, ok := data.GetUserAccessToken(invalid.telegramID); ok {
			t.Fatal("invalid token was bound")
		}
		send(t, message(alice, "/start invalid-token"), "Invalid access token")
		if token, _ := data.GetUserAccessToken(alice.telegramID); token != alice.pat.Token {
			t.Fatal("invalid rebind replaced working PAT")
		}
		_, err := client.NewAuthenticatedClient("invalid-token").GetCurrentUser(ctx)
		requireStatus(t, err, 401)
		spare, err := decodeAPIResponse[memosapi.CreatePersonalAccessTokenResponse](alice.session.api.UserServiceCreatePersonalAccessToken(ctx, strings.TrimPrefix(alice.user.Name, "users/"), memosapi.CreatePersonalAccessTokenRequest{Parent: alice.user.Name, Description: "revoked integration token"}))
		if err != nil {
			t.Fatal(err)
		}
		revoked := alice
		revoked.telegramID = 100
		revoked.auth = client.NewAuthenticatedClient(spare.Token)
		send(t, message(revoked, "/start "+spare.Token), "Hello ")
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, baseURL+"/api/"+spare.PersonalAccessToken.Name, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := alice.auth.httpClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("revoke PAT: HTTP %d", resp.StatusCode)
		}
		_, err = revoked.auth.GetCurrentUser(ctx)
		requireStatus(t, err, 401)
		send(t, message(revoked, "/search integration"), "Invalid access token")
		send(t, message(revoked, "integration revoked must not save"), "Failed to create memo")
		found, err := alice.auth.ListMemos(ctx, buildMemoSearchFilter("integration revoked must not save", alice.user))
		if err != nil || len(found) != 0 {
			t.Fatalf("revoked PAT created a memo: count=%d err=%v", len(found), err)
		}
		send(t, message(revoked, "/start "+spare.Token), "Invalid access token")
	})

	t.Run("empty_and_unbound_messages", func(t *testing.T) {
		send(t, message(alice, ""), "Please input memo content")
		unbound := alice
		unbound.telegramID = 101
		send(t, message(unbound, "must not save"), "Please start the bot")
	})
}
