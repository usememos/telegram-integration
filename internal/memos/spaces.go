package memos

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/usememos/memogram/internal/memosapi"
)

// ErrSpacesUnsupported reports a Memos server without spaces, which arrived
// in the 0.31 series.
var ErrSpacesUnsupported = errors.New("this Memos version does not support spaces")

// maxSpacePages bounds paging in case a server keeps returning page tokens.
const maxSpacePages = 20

// ListSpaces returns the spaces the authenticated user is a member of.
func (c *Client) ListSpaces(ctx context.Context) ([]memosapi.Space, error) {
	var spaces []memosapi.Space
	params := &memosapi.SpaceServiceListSpacesParams{PageSize: 100}
	for range maxSpacePages {
		resp, err := DecodeResponse[memosapi.ListSpacesResponse](c.api.SpaceServiceListSpaces(ctx, params))
		if isMissingEndpoint(err) {
			return nil, ErrSpacesUnsupported
		}
		if err != nil {
			return nil, err
		}
		spaces = append(spaces, resp.Spaces...)
		if resp.NextPageToken == "" {
			break
		}
		params.PageToken = resp.NextPageToken
	}
	return spaces, nil
}

// SupportsSpaces reports whether a Memos version reported by the instance
// profile has spaces. Versions that are not 0.x releases, such as CalVer
// development builds, are assumed to have them; ListSpaces still reports
// ErrSpacesUnsupported if they do not.
func SupportsSpaces(version string) bool {
	major, rest, ok := strings.Cut(strings.TrimPrefix(version, "v"), ".")
	if !ok {
		return true
	}
	minor, _, _ := strings.Cut(rest, ".")
	majorNumber, majorErr := strconv.Atoi(major)
	minorNumber, minorErr := strconv.Atoi(minor)
	if majorErr != nil || minorErr != nil {
		return true
	}
	return majorNumber > 0 || minorNumber >= 31
}
