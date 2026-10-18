package bot

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memos"
	"github.com/usememos/memogram/internal/memosapi"
)

// maxDownloadBytes is the Bot API's limit on files a bot can download.
const maxDownloadBytes = 20 << 20

var errFileTooLarge = errors.New("file exceeds the 20 MiB Telegram download limit")

// telegramFile is the file attached to a message.
type telegramFile struct {
	id string
	// name and mimeType are reported by the sender's client, when known.
	name, mimeType string
}

// messageFile returns the file attached to a message. Photos use the largest
// size Telegram offers.
func messageFile(msg *models.Message) (telegramFile, bool) {
	var file telegramFile
	switch {
	case msg.Document != nil:
		file = telegramFile{id: msg.Document.FileID, name: msg.Document.FileName, mimeType: msg.Document.MimeType}
	case msg.Voice != nil:
		file = telegramFile{id: msg.Voice.FileID, mimeType: msg.Voice.MimeType}
	case msg.Video != nil:
		file = telegramFile{id: msg.Video.FileID, name: msg.Video.FileName, mimeType: msg.Video.MimeType}
	case len(msg.Photo) > 0:
		file = telegramFile{id: msg.Photo[len(msg.Photo)-1].FileID}
	}
	return file, file.id != ""
}

// saveAttachment downloads a file from Telegram and attaches it to memo.
func (s *Service) saveAttachment(ctx context.Context, client *memos.Client, file telegramFile, memo *memosapi.Memo) error {
	info, err := s.bot.GetFile(ctx, &bot.GetFileParams{FileID: file.id})
	if err != nil {
		return fmt.Errorf("get Telegram file: %w", redactTelegramError(err))
	}
	content, contentType, err := s.download(ctx, info)
	if err != nil {
		return err
	}
	_, err = client.CreateAttachment(ctx, memosapi.Attachment{
		Filename: attachmentName(file, info),
		Type:     attachmentType(contentType, file.mimeType, content),
		Content:  base64.StdEncoding.EncodeToString(content),
		Memo:     memo.Name,
	})
	if err != nil {
		return fmt.Errorf("create attachment: %w", err)
	}
	return nil
}

// download reads a Telegram file and returns its bytes and Content-Type.
func (s *Service) download(ctx context.Context, file *models.File) ([]byte, string, error) {
	if file.FileSize > maxDownloadBytes {
		return nil, "", errFileTooLarge
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.bot.FileDownloadLink(file), nil)
	if err != nil {
		return nil, "", fmt.Errorf("create download request: %w", redactTelegramError(err))
	}
	resp, err := s.downloads.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download file: %w", redactTelegramError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("download failed with status %s", resp.Status)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read file: %w", redactTelegramError(err))
	}
	if len(content) > maxDownloadBytes {
		return nil, "", errFileTooLarge
	}
	return content, resp.Header.Get("Content-Type"), nil
}

// attachmentName prefers the sender's file name over Telegram's storage path,
// which names documents like "documents/file_12.pdf".
func attachmentName(file telegramFile, info *models.File) string {
	if file.name != "" {
		switch name := path.Base(strings.ReplaceAll(file.name, `\`, "/")); name {
		case ".", "..", "/":
		default:
			return name
		}
	}
	return path.Base(info.FilePath)
}

// attachmentType prefers a specific Content-Type from the download, then the
// type the sender reported, then content sniffing.
func attachmentType(downloaded, reported string, content []byte) string {
	for _, contentType := range []string{downloaded, reported} {
		if contentType != "" && contentType != "application/octet-stream" {
			return contentType
		}
	}
	return http.DetectContentType(content)
}
