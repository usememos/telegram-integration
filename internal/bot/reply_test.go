package bot

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

// Telegram file URLs and API URLs embed the bot token, and transport errors
// quote the URL. Nothing Memogram logs may contain the token.
func TestTransportErrorsDoNotLeakToken(t *testing.T) {
	const secret = "test-token"

	t.Run("file download", func(t *testing.T) {
		s := newTestService(t, newFakeTelegram(t, nil), Options{Downloads: &http.Client{Transport: failingTransport{}}})
		_, _, err := s.download(context.Background(), &models.File{FilePath: "documents/file_1.pdf"})
		if err == nil || strings.Contains(err.Error(), secret) {
			t.Fatalf("download error leaks the token or is missing: %v", err)
		}
	})

	t.Run("API request", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := listener.Addr().String()
		_ = listener.Close()
		b, err := bot.New("123:"+secret, bot.WithSkipGetMe(), bot.WithServerURL("http://"+addr))
		if err != nil {
			t.Fatal(err)
		}
		_, err = b.SendMessage(context.Background(), &bot.SendMessageParams{ChatID: 1, Text: "hello"})
		if err == nil || strings.Contains(redactTelegramError(err).Error(), secret) {
			t.Fatalf("API error leaks the token or is missing: %v", err)
		}
	})
}
