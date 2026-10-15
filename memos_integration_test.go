package memogram

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/usememos/memogram/internal/memosapi"
	"github.com/usememos/memogram/store"
)

// MEMOS_TEST_URL must point to a fresh, disposable Memos instance.
func TestMemosIntegration(t *testing.T) {
	baseURL := os.Getenv("MEMOS_TEST_URL")
	if baseURL == "" {
		t.Skip("set MEMOS_TEST_URL to a fresh Memos instance to run API compatibility tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	user, err := decodeAPIResponse[memosapi.User](client.api.UserServiceCreateUser(ctx, nil, memosapi.User{
		Username: "memogram-test", Password: "memogram-test-password", Role: memosapi.ADMIN, State: memosapi.UserStateNORMAL, DisplayName: "Memogram Test",
	}))
	if err != nil {
		t.Fatalf("create initial admin: %v", err)
	}
	signIn, err := decodeAPIResponse[memosapi.SignInResponse](client.api.AuthServiceSignIn(ctx, memosapi.SignInRequest{
		PasswordCredentials: memosapi.SignInRequestPasswordCredentials{Username: "memogram-test", Password: "memogram-test-password"},
	}))
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	session := client.NewAuthenticatedClient(signIn.AccessToken)
	pat, err := decodeAPIResponse[memosapi.CreatePersonalAccessTokenResponse](session.api.UserServiceCreatePersonalAccessToken(ctx, strings.TrimPrefix(user.Name, "users/"), memosapi.CreatePersonalAccessTokenRequest{
		Parent: user.Name, Description: "Memogram compatibility test",
	}))
	if err != nil || pat.Token == "" {
		t.Fatalf("create personal access token: %v", err)
	}
	var replies []string
	b := newTestBot(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			replies = append(replies, r.FormValue("text"))
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":2}}`)
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			fmt.Fprint(w, `{"ok":true,"result":{"file_id":"file","file_path":"docs/test.txt","file_size":4}}`)
		case strings.HasPrefix(r.URL.Path, "/file/"):
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "test")
		default:
			t.Errorf("unexpected Telegram request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	data := store.NewStore(filepath.Join(t.TempDir(), "data.txt"))
	if err := data.Init(); err != nil {
		t.Fatal(err)
	}
	s := &Service{bot: b, client: client, config: &Config{ServerAddr: baseURL}, store: data, httpClient: &http.Client{Timeout: 10 * time.Second}, instanceProfile: profile}
	message := &models.Message{ID: 1, Chat: models.Chat{ID: 42, Type: models.ChatTypePrivate}, From: &models.User{ID: 42}}
	message.Text = "/start " + pat.Token
	s.handler(ctx, b, &models.Update{Message: message})
	if _, ok := data.GetUserAccessToken(42); !ok || len(replies) != 1 || !strings.HasPrefix(replies[0], "Hello ") {
		t.Fatalf("bot binding failed: %v", replies)
	}
	message.Text = ""
	message.Caption = "memogram integration needle"
	message.Document = &models.Document{FileID: "file"}
	s.handler(ctx, b, &models.Update{Message: message})
	if len(replies) != 2 || !strings.Contains(replies[1], "Content saved") {
		t.Fatalf("capture with attachment failed: %v", replies)
	}
	auth := client.NewAuthenticatedClient(pat.Token)
	list, err := auth.ListMemos(ctx, buildMemoSearchFilter("integration needle", user))
	if err != nil || len(list) != 1 {
		t.Fatalf("search saved memo: %v", err)
	}
	memo := list[0]
	if memo.Content != message.Caption || memo.Creator != user.Name || memo.Visibility != memosapi.PRIVATE {
		t.Fatalf("unexpected saved memo: %v", memo)
	}
	fetched, err := auth.GetMemo(ctx, memo.Name)
	if err != nil || fetched.Content != memo.Content || fetched.Name != memo.Name {
		t.Fatalf("get memo for keyboard actions: %v", err)
	}
	attachments, err := decodeAPIResponse[memosapi.ListMemoAttachmentsResponse](auth.api.MemoServiceListMemoAttachments(ctx, strings.TrimPrefix(memo.Name, "memos/"), nil))
	if err != nil || len(attachments.Attachments) != 1 {
		t.Fatalf("list saved attachment: %v", err)
	}
	attachment := attachments.Attachments[0]
	if attachment.Memo != memo.Name || attachment.Filename != "test.txt" || attachment.Size != "4" || attachment.Type != "text/plain" {
		t.Fatalf("unexpected saved attachment: %v", attachment)
	}
	memo.Pinned = true
	memo.Visibility = memosapi.PROTECTED
	updated, err := auth.UpdateMemo(ctx, &memo)
	if err != nil || !updated.Pinned || updated.Visibility != memosapi.PROTECTED {
		t.Fatalf("update memo visibility and pin: %v", err)
	}
	memo.Pinned = false
	updated, err = auth.UpdateMemo(ctx, &memo)
	if err != nil || updated.Pinned {
		t.Fatalf("unpin memo: %v", err)
	}
	message.Text = "/search integration needle"
	message.Caption = ""
	message.Document = nil
	s.handler(ctx, b, &models.Update{Message: message})
	if len(replies) != 3 || !strings.Contains(replies[2], memo.Name) || !strings.Contains(replies[2], memo.Content) {
		t.Fatalf("bot search failed: %v", replies)
	}
}
