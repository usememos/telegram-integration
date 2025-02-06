package memogram

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/pkg/errors"
)

func getContentType(imageURL string) (string, error) {
	resp, err := http.Get(imageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Check if the server provided a Content-Type header.
	contentType := resp.Header.Get("Content-Type")
	if contentType != "" && contentType != "application/octet-stream" {
		return contentType, nil
	}

	// Read a few bytes from the body to detect the content type.
	buffer := make([]byte, 512)
	_, err = io.ReadFull(resp.Body, buffer)
	if err != nil && err != io.EOF {
		return "", err
	}

	// Use the DetectContentType function to get the content type.
	contentType = http.DetectContentType(buffer)
	if contentType == "application/octet-stream" {
		// Try to infer content type from URL if detection fails.
		parsedURL, err := url.Parse(imageURL)
		if err == nil {
			contentType = mime.TypeByExtension(path.Ext(parsedURL.Path))
		}
	}
	return contentType, nil
}

// GetNameParentTokens returns the tokens from a resource name.
func GetNameParentTokens(name string, tokenPrefixes ...string) ([]string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 2*len(tokenPrefixes) {
		return nil, errors.Errorf("invalid request %q", name)
	}

	var tokens []string
	for i, tokenPrefix := range tokenPrefixes {
		if fmt.Sprintf("%s/", parts[2*i]) != tokenPrefix {
			return nil, errors.Errorf("invalid prefix %q in request %q", tokenPrefix, name)
		}
		if parts[2*i+1] == "" {
			return nil, errors.Errorf("invalid request %q with empty prefix %q", name, tokenPrefix)
		}
		tokens = append(tokens, parts[2*i+1])
	}
	return tokens, nil
}

// ExtractMemoUIDFromName returns the memo UID from a resource name.
// e.g., "memos/uuid" -> "uuid".
func ExtractMemoUIDFromName(name string) (string, error) {
	tokens, err := GetNameParentTokens(name, "memos/")
	if err != nil {
		return "", err
	}
	id := tokens[0]
	return id, nil
}
