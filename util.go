package memogram

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
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
