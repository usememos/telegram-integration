package memogram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
				if r.Method != http.MethodGet {
					t.Error("profile and current user requests must use GET")
				}
				fmt.Fprint(w, `{"user":{"name":"users/alice","displayName":"Alice"}}`)
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
			wantCalls := 2
			if strings.HasSuffix(namespace, "/v1") {
				wantCalls = 3
			}
			if len(paths) != wantCalls || paths[len(paths)-1] != "/"+namespace+"/auth/me" {
				t.Fatalf("unexpected API discovery: %v", paths)
			}
		})
	}
}

func TestMemosClientDoesNotRetryWritesOrAuthenticationErrors(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.HasSuffix(r.URL.Path, "/instance/profile") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"code":"unauthenticated","message":"denied"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	client := NewMemosClient(server.URL)
	var apiErr *MemosAPIError
	if _, err := client.GetInstanceProfile(context.Background()); !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected authentication error, got %v", err)
	}
	_, err := client.CreateMemo(context.Background(), "test")
	if err == nil || calls != 2 {
		t.Fatalf("writes or authentication failures were retried: calls=%d, err=%v", calls, err)
	}
}
