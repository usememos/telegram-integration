// Package memos is a client for the Memos REST API. It wraps the client
// generated in internal/memosapi.
package memos

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/usememos/memogram/internal/memosapi"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	api        *memosapi.Client
}

// NewClient returns an unauthenticated client for the Memos instance at
// baseURL.
func NewClient(baseURL string) *Client {
	return newClient(baseURL, &http.Client{Timeout: 60 * time.Second, Transport: http.DefaultTransport})
}

func newClient(baseURL string, httpClient *http.Client) *Client {
	api, err := memosapi.NewClient(baseURL, memosapi.WithHTTPClient(httpClient))
	if err != nil {
		panic(err) // Configuration validates the base URL before construction.
	}
	return &Client{baseURL: baseURL, httpClient: httpClient, api: api}
}

// WithAccessToken returns a client that authenticates with a Personal Access
// Token.
func (c *Client) WithAccessToken(accessToken string) *Client {
	httpClient := &http.Client{
		Timeout:   c.httpClient.Timeout,
		Transport: &authTransport{token: accessToken, transport: c.httpClient.Transport},
	}
	return newClient(c.baseURL, httpClient)
}

// BaseURL returns the server address the client was created with.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// API returns the generated client, for operations this package does not wrap.
func (c *Client) API() *memosapi.Client {
	return c.api
}

// HTTPClient returns the HTTP client used for API requests, including
// authentication.
func (c *Client) HTTPClient() *http.Client {
	return c.httpClient
}

// GetInstanceProfile returns the instance's version and public address. It
// needs no authentication.
func (c *Client) GetInstanceProfile(ctx context.Context) (*memosapi.InstanceProfile, error) {
	return DecodeResponse[memosapi.InstanceProfile](c.api.InstanceServiceGetInstanceProfile(ctx))
}

func (c *Client) GetCurrentUser(ctx context.Context) (*memosapi.User, error) {
	resp, err := DecodeResponse[memosapi.GetCurrentUserResponse](c.api.AuthServiceGetCurrentUser(ctx))
	if err != nil {
		return nil, err
	}
	if resp.User.Name == "" {
		return nil, errors.New("memos returned an empty current user")
	}
	return &resp.User, nil
}

// Memo fields that UpdateMemo can save.
const (
	FieldContent    = "content"
	FieldVisibility = "visibility"
	FieldPinned     = "pinned"
	FieldSpace      = "space"
)

// CreateMemo creates a memo owned by the authenticated user. Visibility
// defaults to PRIVATE.
func (c *Client) CreateMemo(ctx context.Context, memo memosapi.Memo) (*memosapi.Memo, error) {
	if memo.Visibility == "" {
		memo.Visibility = memosapi.PRIVATE
	}
	memo.State = memosapi.MemoStateNORMAL
	return DecodeResponse[memosapi.Memo](c.api.MemoServiceCreateMemo(ctx, nil, memo))
}

func (c *Client) GetMemo(ctx context.Context, name string) (*memosapi.Memo, error) {
	id, err := MemoUID(name)
	if err != nil {
		return nil, err
	}
	return DecodeResponse[memosapi.Memo](c.api.MemoServiceGetMemo(ctx, id))
}

// UpdateMemo saves the listed fields of memo, which must name an existing
// memo. Other fields are left unchanged on the server.
func (c *Client) UpdateMemo(ctx context.Context, memo *memosapi.Memo, fields ...string) (*memosapi.Memo, error) {
	if len(fields) == 0 {
		return nil, errors.New("update memo: no fields to update")
	}
	id, err := MemoUID(memo.Name)
	if err != nil {
		return nil, err
	}
	// Send only fields that UpdateMemo can save. The schema requires
	// visibility and state even when the mask leaves them out.
	body := memosapi.Memo{
		Content:    memo.Content,
		Visibility: memo.Visibility,
		Pinned:     memo.Pinned,
		Space:      memo.Space,
		State:      memosapi.MemoStateNORMAL,
	}
	return DecodeResponse[memosapi.Memo](c.api.MemoServiceUpdateMemo(ctx, id,
		&memosapi.MemoServiceUpdateMemoParams{UpdateMask: strings.Join(fields, ",")}, body))
}

func (c *Client) ListMemos(ctx context.Context, filter string) ([]memosapi.Memo, error) {
	resp, err := DecodeResponse[memosapi.ListMemosResponse](c.api.MemoServiceListMemos(ctx, &memosapi.MemoServiceListMemosParams{PageSize: 10, Filter: filter, State: "NORMAL"}))
	if err != nil {
		return nil, err
	}
	return resp.Memos, nil
}

func (c *Client) CreateAttachment(ctx context.Context, attachment memosapi.Attachment) (*memosapi.Attachment, error) {
	return DecodeResponse[memosapi.Attachment](c.api.AttachmentServiceCreateAttachment(ctx, nil, attachment))
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

// NormalizeBaseURL validates a Memos server address and returns it as an
// HTTP(S) URL without a trailing slash. A missing scheme defaults to http, and
// the legacy gRPC-style "dns:" prefix is accepted.
func NormalizeBaseURL(addr string) (string, error) {
	addr = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(addr), "dns:"))
	if addr == "" {
		return "", errors.New("must not be empty")
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("must be an HTTP or HTTPS base URL without credentials, query or fragment")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// SearchFilter returns a Memos filter for memos created by user that contain
// text. The creator condition keeps other users' public memos out of the
// results.
func SearchFilter(text string, user *memosapi.User) string {
	filter := fmt.Sprintf("content.contains(%q)", text)
	if user == nil {
		return filter
	}
	creator := user.Name
	if creator == "" && user.Username != "" {
		creator = "users/" + user.Username
	}
	if creator == "" {
		return filter
	}
	return fmt.Sprintf("%s && creator == %q", filter, creator)
}

// MemoUID returns the ID segment of a memo resource name, such as "abc" for
// "memos/abc".
func MemoUID(name string) (string, error) {
	uid, ok := strings.CutPrefix(name, "memos/")
	if !ok || uid == "" || strings.Contains(uid, "/") {
		return "", fmt.Errorf("invalid memo name %q", name)
	}
	return uid, nil
}
