package bot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memosapi"
)

// attachmentMemos fakes memo creation and records attachment uploads.
func attachmentMemos(t *testing.T, uploads *atomic.Int32, fail bool, got *memosapi.Attachment) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uploads.Add(1)
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"code":"internal","message":"storage unavailable"}`)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(got); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"name":"attachments/test-file"}`)
	}
}

func TestHandlerAttachmentFeedback(t *testing.T) {
	for _, failure := range []string{"", "getFile", "download", "upload", "reportedSize", "downloadedSize"} {
		t.Run(failure, func(t *testing.T) {
			file := fakeFile{path: "docs/test.txt", contentType: "text/plain", content: []byte("test")}
			switch failure {
			case "download":
				file.status = http.StatusBadGateway
			case "reportedSize":
				file.reportedSize = maxDownloadBytes + 1
			case "downloadedSize":
				file.content = []byte(strings.Repeat("a", maxDownloadBytes+1))
			}
			files := map[string]fakeFile{"file": file}
			if failure == "getFile" {
				files = nil
			}
			tg := newFakeTelegram(t, files)
			var uploads atomic.Int32
			var attachment memosapi.Attachment
			data := openStore(t)
			bindUser(t, data, 1, "test-token")
			s := newTestService(t, tg, Options{
				Memos: fakeMemos(t, map[string]http.HandlerFunc{
					"POST /api/memos":       respond(`{"name":"memos/test-memo","visibility":"PRIVATE"}`),
					"POST /api/attachments": attachmentMemos(t, &uploads, failure == "upload", &attachment),
				}),
				Store: data,
			})
			s.process(&models.Update{Message: &models.Message{
				ID: 1, Chat: models.Chat{ID: 1}, From: &models.User{ID: 1},
				Caption: "caption", Document: &models.Document{FileID: "file"},
			}})

			calls := tg.snapshot()
			if len(calls) != 1 {
				t.Fatalf("expected one reply, got %+v", calls)
			}
			reply := calls[0].text
			if failure == "" {
				if !strings.Contains(reply, "Content saved") || uploads.Load() != 1 {
					t.Fatalf("missing success confirmation: %q, uploads=%d", reply, uploads.Load())
				}
				if attachment.Content != "dGVzdA==" || attachment.Memo != "memos/test-memo" || attachment.Filename != "test.txt" || attachment.Type != "text/plain" {
					t.Fatalf("incorrect attachment request: %+v", attachment)
				}
			} else if !strings.Contains(reply, "Memo saved, but the attachment failed") || strings.Contains(reply, "Content saved") {
				t.Fatalf("attachment failure reported as success: %q", reply)
			}
			if failure == "reportedSize" && tg.downloadCount() != 0 {
				t.Fatal("oversized file should be rejected before download")
			}
			if failure == "downloadedSize" && uploads.Load() != 0 {
				t.Fatal("oversized download should not be uploaded")
			}
			if !strings.Contains(reply, s.memos.BaseURL()+"/memos/test-memo") {
				t.Fatalf("confirmation should link to the saved memo: %q", reply)
			}
		})
	}
}

func TestAttachmentKeepsSenderFileNameAndType(t *testing.T) {
	// Telegram's file server stores documents under generic names and does
	// not report a specific Content-Type.
	tg := newFakeTelegram(t, map[string]fakeFile{"file": {path: "documents/file_12", contentType: "application/octet-stream", content: []byte("PK\x03\x04docx")}})
	var uploads atomic.Int32
	var attachment memosapi.Attachment
	data := openStore(t)
	bindUser(t, data, 1, "test-token")
	s := newTestService(t, tg, Options{
		Memos: fakeMemos(t, map[string]http.HandlerFunc{
			"POST /api/memos":       respond(`{"name":"memos/test-memo","visibility":"PRIVATE"}`),
			"POST /api/attachments": attachmentMemos(t, &uploads, false, &attachment),
		}),
		Store: data,
	})
	const docx = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	s.process(&models.Update{Message: &models.Message{
		ID: 1, Chat: models.Chat{ID: 1}, From: &models.User{ID: 1},
		Document: &models.Document{FileID: "file", FileName: "Quarterly report.docx", MimeType: docx},
	}})
	if attachment.Filename != "Quarterly report.docx" || attachment.Type != docx {
		t.Fatalf("lost sender metadata: %+v", attachment)
	}
}

func TestMessageFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  models.Message
		want telegramFile
		ok   bool
	}{
		{"none", models.Message{Text: "text"}, telegramFile{}, false},
		{"document", models.Message{Document: &models.Document{FileID: "d", FileName: "a.pdf", MimeType: "application/pdf"}}, telegramFile{id: "d", name: "a.pdf", mimeType: "application/pdf"}, true},
		{"document wins over photo", models.Message{Document: &models.Document{FileID: "d"}, Photo: []models.PhotoSize{{FileID: "p"}}}, telegramFile{id: "d"}, true},
		{"voice", models.Message{Voice: &models.Voice{FileID: "v", MimeType: "audio/ogg"}}, telegramFile{id: "v", mimeType: "audio/ogg"}, true},
		{"video", models.Message{Video: &models.Video{FileID: "m", FileName: "clip.mp4"}}, telegramFile{id: "m", name: "clip.mp4"}, true},
		{"largest photo", models.Message{Photo: []models.PhotoSize{{FileID: "small"}, {FileID: "large"}}}, telegramFile{id: "large"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := messageFile(&tc.msg); got != tc.want || ok != tc.ok {
				t.Fatalf("got %+v, %v; want %+v, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestAttachmentName(t *testing.T) {
	info := &models.File{FilePath: "documents/file_12.pdf"}
	for name, want := range map[string]string{
		"":              "file_12.pdf",
		"report.pdf":    "report.pdf",
		"../report.pdf": "report.pdf",
		`dir\notes.txt`: "notes.txt",
		"..":            "file_12.pdf",
		"/":             "file_12.pdf",
	} {
		if got := attachmentName(telegramFile{name: name}, info); got != want {
			t.Errorf("attachmentName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestAttachmentType(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n")
	for _, tc := range []struct{ downloaded, reported, want string }{
		{"image/png", "image/jpeg", "image/png"},
		{"application/octet-stream", "image/jpeg", "image/jpeg"},
		{"", "", "image/png"},
		{"application/octet-stream", "application/octet-stream", "image/png"},
	} {
		if got := attachmentType(tc.downloaded, tc.reported, png); got != tc.want {
			t.Errorf("attachmentType(%q, %q) = %q, want %q", tc.downloaded, tc.reported, got, tc.want)
		}
	}
}
