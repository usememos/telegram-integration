package memos

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
)

// APIError is a non-2xx response from Memos.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("Memos API returned HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("Memos API returned HTTP %d", e.StatusCode)
}

// hasStatus reports whether err is an *APIError with one of the given status
// codes.
func hasStatus(err error, codes ...int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && slices.Contains(codes, apiErr.StatusCode)
}

// isMissingEndpoint reports a server that does not implement the requested
// endpoint.
func isMissingEndpoint(err error) bool {
	return hasStatus(err, http.StatusNotFound, http.StatusNotImplemented)
}

// RejectsSpace reports a create error that a missing, malformed or forbidden
// space can cause. Memos reports a space the user is not a member of as not
// found.
func RejectsSpace(err error) bool {
	return hasStatus(err, http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound)
}

// DecodeResponse closes resp and decodes a successful JSON body into T. A
// non-2xx status becomes an *APIError carrying the server's message.
func DecodeResponse[T any](resp *http.Response, err error) (*T, error) {
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var status struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&status)
		return nil, &APIError{StatusCode: resp.StatusCode, Message: status.Message}
	}
	var result T
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Memos API response: %w", err)
	}
	return &result, nil
}
