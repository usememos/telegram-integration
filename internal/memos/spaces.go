package memos

import (
	"context"

	"github.com/usememos/memogram/internal/memosapi"
)

// maxSpacePages bounds paging in case a server keeps returning page tokens.
const maxSpacePages = 20

// ListSpaces returns the spaces the authenticated user is a member of.
func (c *Client) ListSpaces(ctx context.Context) ([]memosapi.Space, error) {
	var spaces []memosapi.Space
	params := &memosapi.SpaceServiceListSpacesParams{PageSize: 100}
	for range maxSpacePages {
		resp, err := DecodeResponse[memosapi.ListSpacesResponse](c.api.SpaceServiceListSpaces(ctx, params))
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
