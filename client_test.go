package memogram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/usememos/memogram/internal/memosapi"
)

func TestMemosClientAPINamespace(t *testing.T) {
	for _, namespace := range []string{"api", "api/v1", "notes/api", "notes/api/v1"} {
		t.Run(namespace, func(t *testing.T) {
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if !strings.HasPrefix(r.URL.Path, "/"+namespace+"/") {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/instance/profile") {
					fmt.Fprint(w, `{"version":"test","instanceUrl":"https://memos.example.com"}`)
					return
				}
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing access token")
				}
				switch r.Method + " " + strings.TrimPrefix(r.URL.Path, "/"+namespace) {
				case "GET /auth/me":
					fmt.Fprint(w, `{"user":{"name":"users/alice","displayName":"Alice"}}`)
				case "POST /memos", "PATCH /memos/test-memo":
					var memo memosapi.Memo
					if err := json.NewDecoder(r.Body).Decode(&memo); err != nil || memo.Content != "test" || memo.Visibility != memosapi.PRIVATE {
						t.Errorf("unexpected memo request: %+v, %v", memo, err)
					}
					if r.Method == http.MethodPatch && r.URL.Query().Get("updateMask") != "visibility,pinned" {
						t.Error("missing update mask")
					}
					fmt.Fprint(w, `{"name":"memos/test-memo","content":"test","visibility":"PRIVATE"}`)
				case "POST /attachments":
					var attachment memosapi.Attachment
					if err := json.NewDecoder(r.Body).Decode(&attachment); err != nil || attachment.Content != "dGVzdA==" || attachment.Memo != "memos/test-memo" {
						t.Errorf("unexpected attachment request: %+v, %v", attachment, err)
					}
					fmt.Fprint(w, `{"name":"attachments/test-file"}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			baseURL := server.URL
			if strings.HasPrefix(namespace, "notes/") {
				baseURL += "/notes"
			}
			client := NewMemosClient(baseURL)
			// An authenticated client created before discovery must share the selection.
			auth := client.NewAuthenticatedClient("test-token")
			profile, err := client.GetInstanceProfile(context.Background())
			if err != nil || profile.InstanceUrl != "https://memos.example.com" {
				t.Fatalf("get profile: %v, %v", profile, err)
			}
			user, err := auth.GetCurrentUser(context.Background())
			if err != nil || user.DisplayName != "Alice" {
				t.Fatalf("get current user: %v, %v", user, err)
			}
			memo, err := auth.CreateMemo(context.Background(), "test")
			if err != nil || memo.Name != "memos/test-memo" {
				t.Fatalf("create memo: %v, %v", memo, err)
			}
			if _, err := auth.UpdateMemo(context.Background(), memo); err != nil {
				t.Fatalf("update memo: %v", err)
			}
			if _, err := auth.CreateAttachment(context.Background(), memosapi.Attachment{Filename: "test.txt", Type: "text/plain", Content: "dGVzdA==", Memo: memo.Name}); err != nil {
				t.Fatalf("upload attachment: %v", err)
			}
			wantCalls := 5
			if strings.HasSuffix(namespace, "/v1") {
				wantCalls = 6
			}
			if len(paths) != wantCalls || paths[len(paths)-1] != "/"+namespace+"/attachments" {
				t.Fatalf("unexpected API discovery: %v", paths)
			}
		})
	}
}

func TestMemosClientProfileFallback(t *testing.T) {
	for _, status := range []int{404, 501, 401, 403, 500, 502} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v1/instance/profile" {
					fmt.Fprint(w, `{"version":"legacy"}`)
					return
				}
				w.WriteHeader(status)
				fmt.Fprint(w, `{"message":"profile failed"}`)
			}))
			defer server.Close()
			client := NewMemosClient(server.URL)
			profile, err := client.GetInstanceProfile(context.Background())
			if status == 404 || status == 501 {
				if err != nil || profile.Version != "legacy" || len(paths) != 2 || paths[0] != "/api/instance/profile" || paths[1] != "/api/v1/instance/profile" {
					t.Fatalf("fallback failed: paths=%v err=%v", paths, err)
				}
			} else {
				var apiErr *MemosAPIError
				if !errors.As(err, &apiErr) || apiErr.StatusCode != status || len(paths) != 1 {
					t.Fatalf("error unexpectedly retried: paths=%v err=%v", paths, err)
				}
			}
		})
	}
}

func TestMemosClientFailedFallbackResetsNamespace(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch len(paths) {
		case 1:
			w.WriteHeader(http.StatusNotFound)
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			fmt.Fprint(w, `{"version":"recovered"}`)
		}
	}))
	defer server.Close()
	client := NewMemosClient(server.URL)
	if _, err := client.GetInstanceProfile(context.Background()); err == nil {
		t.Fatal("expected fallback failure")
	}
	profile, err := client.GetInstanceProfile(context.Background())
	if err != nil || profile.Version != "recovered" || strings.Join(paths, ",") != "/api/instance/profile,/api/v1/instance/profile,/api/instance/profile" {
		t.Fatalf("namespace did not recover: paths=%v err=%v", paths, err)
	}
}

func TestMemosClientTimeoutDoesNotFallback(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		<-r.Context().Done()
	}))
	defer server.Close()
	client := NewMemosClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := client.GetInstanceProfile(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout, got %v", err)
	}
	// Closing waits for the handler, so reading its request log is race-free.
	server.Close()
	if len(paths) != 1 || paths[0] != "/api/instance/profile" {
		t.Fatalf("timeout triggered namespace discovery: %v", paths)
	}
}

func TestMemosClientDoesNotRetryWrites(t *testing.T) {
	for _, write := range []struct {
		name, method, path string
		call               func(*MemosClient) error
	}{
		{"create", "POST", "/api/memos", func(c *MemosClient) error { _, err := c.CreateMemo(context.Background(), "test"); return err }},
		{"update", "PATCH", "/api/memos/test", func(c *MemosClient) error {
			_, err := c.UpdateMemo(context.Background(), &memosapi.Memo{Name: "memos/test", Visibility: memosapi.PRIVATE})
			return err
		}},
		{"attachment", "POST", "/api/attachments", func(c *MemosClient) error {
			_, err := c.CreateAttachment(context.Background(), memosapi.Attachment{Filename: "test.txt", Type: "text/plain", Content: "dGVzdA=="})
			return err
		}},
	} {
		for _, status := range []int{404, 501, 500} {
			t.Run(fmt.Sprintf("%s/%d", write.name, status), func(t *testing.T) {
				var calls int
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Method != write.method || r.URL.Path != write.path {
						t.Errorf("unexpected write: %s %s", r.Method, r.URL.Path)
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					fmt.Fprint(w, `{"message":"write failed"}`)
				}))
				defer server.Close()
				var apiErr *MemosAPIError
				if err := write.call(NewMemosClient(server.URL)); !errors.As(err, &apiErr) || apiErr.StatusCode != status || calls != 1 {
					t.Fatalf("write was retried or error lost: calls=%d err=%v", calls, err)
				}
			})
		}
	}
}
