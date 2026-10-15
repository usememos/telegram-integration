package memogram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/usememos/memogram/internal/memosapi"
)

//go:generate sh openapi/generate.sh

type MemosClient struct {
	baseURL    string
	httpClient *http.Client
	legacyAPI  *atomic.Bool
	api        *memosapi.Client
}

// NewMemosClient uses the REST client generated from dotcom's OpenAPI snapshot.
// Call GetInstanceProfile at startup to select /api or /api/v1 before writing.
func NewMemosClient(baseURL string) *MemosClient {
	legacyAPI := &atomic.Bool{}
	u, _ := url.Parse(baseURL)
	httpClient := &http.Client{
		Timeout:   60 * time.Second,
		Transport: &apiTransport{legacyAPI: legacyAPI, basePath: strings.TrimRight(u.Path, "/"), transport: http.DefaultTransport},
	}
	return newMemosClient(baseURL, httpClient, legacyAPI)
}

func newMemosClient(baseURL string, httpClient *http.Client, legacyAPI *atomic.Bool) *MemosClient {
	api, err := memosapi.NewClient(baseURL, memosapi.WithHTTPClient(httpClient))
	if err != nil {
		panic(err) // Service configuration validates the base URL before construction.
	}
	return &MemosClient{baseURL: baseURL, httpClient: httpClient, legacyAPI: legacyAPI, api: api}
}

// GetInstanceProfile discovers the API base path using a read-only request.
// Writes are never retried as part of version discovery.
func (c *MemosClient) GetInstanceProfile(ctx context.Context) (*memosapi.InstanceProfile, error) {
	profile, err := decodeAPIResponse[memosapi.InstanceProfile](c.api.InstanceServiceGetInstanceProfile(ctx))
	var apiErr *MemosAPIError
	if !c.legacyAPI.Load() && errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusNotImplemented) {
		c.legacyAPI.Store(true)
		profile, err = decodeAPIResponse[memosapi.InstanceProfile](c.api.InstanceServiceGetInstanceProfile(ctx))
		if err != nil {
			c.legacyAPI.Store(false)
		}
	}
	return profile, err
}

func (c *MemosClient) NewAuthenticatedClient(accessToken string) *MemosClient {
	httpClient := &http.Client{
		Timeout:   c.httpClient.Timeout,
		Transport: &authTransport{token: accessToken, transport: c.httpClient.Transport},
	}
	return newMemosClient(c.baseURL, httpClient, c.legacyAPI)
}

func (c *MemosClient) GetCurrentUser(ctx context.Context) (*memosapi.User, error) {
	resp, err := decodeAPIResponse[memosapi.GetCurrentUserResponse](c.api.AuthServiceGetCurrentUser(ctx))
	if err != nil {
		return nil, err
	}
	if resp.User.Name == "" {
		return nil, fmt.Errorf("Memos returned an empty current user")
	}
	return &resp.User, nil
}

func (c *MemosClient) CreateMemo(ctx context.Context, content string) (*memosapi.Memo, error) {
	return decodeAPIResponse[memosapi.Memo](c.api.MemoServiceCreateMemo(ctx, nil, memosapi.Memo{
		Content: content, Visibility: memosapi.PRIVATE, State: memosapi.MemoStateNORMAL,
	}))
}

func (c *MemosClient) GetMemo(ctx context.Context, name string) (*memosapi.Memo, error) {
	id, err := ExtractMemoUIDFromName(name)
	if err != nil {
		return nil, err
	}
	return decodeAPIResponse[memosapi.Memo](c.api.MemoServiceGetMemo(ctx, id))
}

func (c *MemosClient) UpdateMemo(ctx context.Context, memo *memosapi.Memo) (*memosapi.Memo, error) {
	id, err := ExtractMemoUIDFromName(memo.Name)
	if err != nil {
		return nil, err
	}
	return decodeAPIResponse[memosapi.Memo](c.api.MemoServiceUpdateMemo(ctx, id,
		&memosapi.MemoServiceUpdateMemoParams{UpdateMask: "visibility,pinned"},
		memosapi.Memo{Content: memo.Content, Visibility: memo.Visibility, State: memosapi.MemoStateNORMAL, Pinned: memo.Pinned},
	))
}

func (c *MemosClient) ListMemos(ctx context.Context, filter string) ([]memosapi.Memo, error) {
	resp, err := decodeAPIResponse[memosapi.ListMemosResponse](c.api.MemoServiceListMemos(ctx, &memosapi.MemoServiceListMemosParams{PageSize: 10, Filter: filter, State: "NORMAL"}))
	if err != nil {
		return nil, err
	}
	return resp.Memos, nil
}

func (c *MemosClient) CreateAttachment(ctx context.Context, attachment memosapi.Attachment) (*memosapi.Attachment, error) {
	return decodeAPIResponse[memosapi.Attachment](c.api.AttachmentServiceCreateAttachment(ctx, nil, attachment))
}

type MemosAPIError struct {
	StatusCode int
	Message    string
}

func (e *MemosAPIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("Memos API returned HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("Memos API returned HTTP %d", e.StatusCode)
}

func decodeAPIResponse[T any](resp *http.Response, err error) (*T, error) {
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var status struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&status)
		return nil, &MemosAPIError{StatusCode: resp.StatusCode, Message: status.Message}
	}
	var result T
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Memos API response: %w", err)
	}
	return &result, nil
}

type apiTransport struct {
	legacyAPI *atomic.Bool
	basePath  string
	transport http.RoundTripper
}

func (t *apiTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if t.legacyAPI.Load() {
		prefix := t.basePath + "/api/"
		req.URL.Path = strings.Replace(req.URL.Path, prefix, t.basePath+"/api/v1/", 1)
		if req.URL.RawPath != "" {
			req.URL.RawPath = strings.Replace(req.URL.RawPath, prefix, t.basePath+"/api/v1/", 1)
		}
	}
	return t.transport.RoundTrip(req)
}

type authTransport struct {
	token     string
	transport http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.transport.RoundTrip(req)
}
