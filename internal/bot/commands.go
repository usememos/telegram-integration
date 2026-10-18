package bot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memos"
)

// handleStart binds the sender to a Memos Personal Access Token after
// checking that the token works.
func (s *Service) handleStart(ctx context.Context, _ *bot.Bot, update *models.Update) {
	msg := update.Message
	accessToken := commandArgs(msg.Text, commandStart)
	if accessToken == "" {
		s.reply(ctx, msg.Chat.ID, "Usage: /start <access_token>")
		return
	}
	user, err := s.memos.WithAccessToken(accessToken).GetCurrentUser(ctx)
	if err != nil {
		slog.Warn("failed to verify access token", slog.Any("err", err))
		s.reply(ctx, msg.Chat.ID, "Invalid access token")
		return
	}
	if err := s.store.SetAccessToken(msg.From.ID, accessToken); err != nil {
		slog.Error("failed to save access token", slog.Any("err", err))
		s.reply(ctx, msg.Chat.ID, "Failed to save access token, please try again")
		return
	}
	s.reply(ctx, msg.Chat.ID, fmt.Sprintf("Hello %s!", user.DisplayName))
}

// handleSearch replies with the sender's memos that contain the given words,
// one message per memo.
func (s *Service) handleSearch(ctx context.Context, _ *bot.Bot, update *models.Update) {
	msg := update.Message
	query := commandArgs(msg.Text, commandSearch)
	if query == "" {
		s.reply(ctx, msg.Chat.ID, "Usage: /search <words>")
		return
	}
	accessToken, ok := s.store.AccessToken(msg.From.ID)
	if !ok {
		s.reply(ctx, msg.Chat.ID, startFirst)
		return
	}
	client := s.memos.WithAccessToken(accessToken)
	user, err := client.GetCurrentUser(ctx)
	if err != nil {
		slog.Warn("failed to verify access token", slog.Any("err", err))
		s.reply(ctx, msg.Chat.ID, "Invalid access token")
		return
	}
	found, err := client.ListMemos(ctx, memos.SearchFilter(query, user))
	if err != nil {
		slog.Error("failed to search memos", slog.Any("err", err))
		s.reply(ctx, msg.Chat.ID, "Failed to search memos")
		return
	}
	if len(found) == 0 {
		s.reply(ctx, msg.Chat.ID, "No memos found for the specified search criteria.")
		return
	}
	for _, memo := range found {
		s.reply(ctx, msg.Chat.ID, memo.Name+"\n"+memo.Content)
	}
}
