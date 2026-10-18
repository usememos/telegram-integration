package bot

import (
	"context"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// authorize runs before every handler. It drops updates Memogram does not
// handle, such as edits, channel posts and membership changes, and enforces
// ALLOWED_USERNAMES for both messages and button presses.
func (s *Service) authorize(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		switch {
		case update.Message != nil:
			msg := update.Message
			if msg.From == nil {
				return
			}
			if reason, ok := s.checkAccess(msg.From.Username); !ok {
				slog.Warn("access denied", slog.String("username", msg.From.Username))
				s.reply(ctx, msg.Chat.ID, "Error: "+reason)
				return
			}
		case update.CallbackQuery != nil:
			query := update.CallbackQuery
			if reason, ok := s.checkAccess(query.From.Username); !ok {
				slog.Warn("access denied", slog.String("username", query.From.Username))
				s.alert(ctx, query.ID, "Error: "+reason)
				return
			}
		default:
			return
		}
		next(ctx, b, update)
	}
}

// checkAccess reports whether a Telegram username may use the bot, and why
// not when it may not.
func (s *Service) checkAccess(username string) (string, bool) {
	if len(s.allowed) == 0 {
		return "", true
	}
	if username == "" {
		return "your account must have a username to use this bot", false
	}
	if _, ok := s.allowed[strings.ToLower(strings.TrimSpace(username))]; !ok {
		return "your account " + username + " is not allowed to use this bot", false
	}
	return "", true
}
