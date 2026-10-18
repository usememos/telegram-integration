package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const startFirst = "Please start the bot with /start <access_token>"

// The helpers below log Telegram failures, which are otherwise invisible to
// both the user and the operator.

func (s *Service) reply(ctx context.Context, chatID int64, text string) {
	s.send(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text})
}

func (s *Service) send(ctx context.Context, params *bot.SendMessageParams) {
	if _, err := s.bot.SendMessage(ctx, params); err != nil {
		slog.Error("failed to send Telegram message", slog.Any("err", redactTelegramError(err)))
	}
}

func (s *Service) edit(ctx context.Context, params *bot.EditMessageTextParams) {
	if _, err := s.bot.EditMessageText(ctx, params); err != nil {
		slog.Error("failed to edit Telegram message", slog.Any("err", redactTelegramError(err)))
	}
}

// editMarkup replaces a message's buttons.
func (s *Service) editMarkup(ctx context.Context, msg *models.Message, keyboard *models.InlineKeyboardMarkup) {
	if _, err := s.bot.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{ChatID: msg.Chat.ID, MessageID: msg.ID, ReplyMarkup: keyboard}); err != nil {
		slog.Error("failed to edit Telegram buttons", slog.Any("err", redactTelegramError(err)))
	}
}

// answer acknowledges a button press with a brief notification.
func (s *Service) answer(ctx context.Context, queryID, text string) {
	s.answerCallback(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: queryID, Text: text})
}

// alert acknowledges a button press with a dialog the user must dismiss.
func (s *Service) alert(ctx context.Context, queryID, text string) {
	s.answerCallback(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: queryID, Text: text, ShowAlert: true})
}

func (s *Service) answerCallback(ctx context.Context, params *bot.AnswerCallbackQueryParams) {
	if _, err := s.bot.AnswerCallbackQuery(ctx, params); err != nil {
		slog.Error("failed to answer Telegram callback", slog.Any("err", redactTelegramError(err)))
	}
}

// markdownEscaper escapes the characters that Telegram's legacy Markdown
// treats as formatting, for user-provided text such as space titles.
var markdownEscaper = strings.NewReplacer("_", `\_`, "*", `\*`, "`", "\\`", "[", `\[`)

func escapeMarkdown(text string) string {
	return markdownEscaper.Replace(text)
}

// redactTelegramError drops request URLs, which contain the bot token, from
// transport errors so that they can be logged safely.
func redactTelegramError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%s request failed: %w", urlErr.Op, urlErr.Err)
	}
	return err
}
