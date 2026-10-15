package memogram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/usememos/memogram/store"
)

func newTestBot(t *testing.T, handler http.HandlerFunc) *bot.Bot {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	b, err := bot.New("123:test-token", bot.WithSkipGetMe(), bot.WithServerURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHandlerAttachmentFeedback(t *testing.T) {
	for _, failure := range []string{"", "getFile", "download", "upload", "reportedSize", "downloadedSize"} {
		t.Run(failure, func(t *testing.T) {
			var replies []string
			var uploads, downloads int
			b := newTestBot(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/getFile"):
					if failure == "getFile" {
						fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"file unavailable"}`)
						return
					}
					size := 4
					if failure == "reportedSize" {
						size = maxDownloadBytes + 1
					}
					fmt.Fprintf(w, `{"ok":true,"result":{"file_id":"file","file_path":"docs/test.txt","file_size":%d}}`, size)
				case strings.HasPrefix(r.URL.Path, "/file/"):
					downloads++
					if failure == "download" {
						w.WriteHeader(http.StatusBadGateway)
						return
					}
					w.Header().Set("Content-Type", "text/plain")
					if failure == "downloadedSize" {
						io.Copy(w, strings.NewReader(strings.Repeat("a", maxDownloadBytes+1)))
						return
					}
					fmt.Fprint(w, "test")
				case strings.HasSuffix(r.URL.Path, "/sendMessage"):
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
					}
					replies = append(replies, r.FormValue("text"))
					fmt.Fprint(w, `{"ok":true,"result":{"message_id":2}}`)
				default:
					t.Errorf("unexpected Telegram request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})
			memos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/memos":
					fmt.Fprint(w, `{"name":"memos/test-memo","visibility":"PRIVATE"}`)
				case "/api/attachments":
					uploads++
					if failure == "upload" {
						w.WriteHeader(http.StatusInternalServerError)
						fmt.Fprint(w, `{"code":"internal","message":"storage unavailable"}`)
						return
					}
					var request struct {
						Content []byte `json:"content"`
						Memo    string `json:"memo"`
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil || string(request.Content) != "test" || request.Memo != "memos/test-memo" {
						t.Errorf("incorrect attachment request: %+v, %v", request, err)
					}
					fmt.Fprint(w, `{"name":"attachments/test-file"}`)
				default:
					t.Errorf("unexpected Memos request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer memos.Close()
			data := store.NewStore(filepath.Join(t.TempDir(), "data.txt"))
			data.SetUserAccessToken(1, "test-token")
			s := &Service{bot: b, client: NewMemosClient(memos.URL), config: &Config{ServerAddr: memos.URL}, store: data, httpClient: memos.Client()}
			s.handler(context.Background(), b, &models.Update{Message: &models.Message{
				ID: 1, Chat: models.Chat{ID: 1}, From: &models.User{ID: 1},
				Caption: "caption", Document: &models.Document{FileID: "file"},
			}})
			if len(replies) != 1 {
				t.Fatalf("expected one reply, got %v", replies)
			}
			if failure == "" {
				if !strings.Contains(replies[0], "Content saved") || uploads != 1 {
					t.Fatalf("missing success confirmation: %v, uploads=%d", replies, uploads)
				}
			} else if !strings.Contains(replies[0], "Memo saved, but the attachment failed") || strings.Contains(replies[0], "Content saved") {
				t.Fatalf("attachment failure reported as success: %v", replies)
			}
			if failure == "reportedSize" && downloads != 0 {
				t.Fatal("oversized file should be rejected before download")
			}
			if failure == "downloadedSize" && uploads != 0 {
				t.Fatal("oversized download should not be uploaded")
			}
			if !strings.Contains(replies[0], memos.URL+"/memos/test-memo") {
				t.Fatalf("confirmation should link to the saved memo: %v", replies)
			}
		})
	}
}
