package bot

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memos"
	"github.com/usememos/memogram/internal/memosapi"
	"github.com/usememos/memogram/internal/store"
)

const (
	spacesUnsupported = "Your Memos version does not support spaces (0.31 or later is required)"
	noSpaces          = "You are not a member of any space."

	// noSpaceKey is the space key of the "No space" choice.
	noSpaceKey = "-"
	// maxPickerSpaces leaves room for the picker's own buttons within
	// Telegram's limit of 100 buttons per keyboard.
	maxPickerSpaces = 96
)

// spaceKey identifies a space in button data. Memo and space IDs can each be
// 36 characters, so a button cannot carry both within Telegram's 64 byte
// limit. The key is resolved against the user's spaces when pressed.
func spaceKey(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:4])
}

// findSpace resolves a space key. It fails when the key matches no space or,
// improbably, several, so a changed list never picks the wrong space.
func findSpace(spaces []memosapi.Space, key string) (memosapi.Space, bool) {
	var found []memosapi.Space
	for _, space := range spaces {
		if spaceKey(space.Name) == key {
			found = append(found, space)
		}
	}
	if len(found) != 1 {
		return memosapi.Space{}, false
	}
	return found[0], true
}

func spaceTitle(space memosapi.Space) string {
	return cmp.Or(strings.TrimSpace(space.Title), space.Name)
}

// spacePicker returns a keyboard with one button per space, marking the
// current one, followed by a row of extra buttons.
func spacePicker(spaces []memosapi.Space, current string, data func(key string) callbackData, extra ...models.InlineKeyboardButton) *models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton
	for _, space := range spaces[:min(len(spaces), maxPickerSpaces)] {
		text := spaceTitle(space)
		if space.Name == current {
			text = "✓ " + text
		}
		rows = append(rows, []models.InlineKeyboardButton{dataButton(text, data(spaceKey(space.Name)))})
	}
	if len(extra) > 0 {
		rows = append(rows, extra)
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// listSpaces lists the user's spaces and remembers when the server turns out
// not to support them, so their buttons stop being offered.
func (s *Service) listSpaces(ctx context.Context, client *memos.Client) ([]memosapi.Space, error) {
	spaces, err := client.ListSpaces(ctx)
	if errors.Is(err, memos.ErrSpacesUnsupported) {
		s.spaces.Store(false)
	}
	return spaces, err
}

func (s *Service) alertSpacesError(ctx context.Context, queryID string, err error) {
	s.alert(ctx, queryID, spacesErrorText(err))
}

// spacesErrorText returns the reply for a failure to list spaces, logging
// failures other than an unsupported server.
func spacesErrorText(err error) string {
	if errors.Is(err, memos.ErrSpacesUnsupported) {
		return spacesUnsupported
	}
	slog.Error("failed to list spaces", slog.Any("err", err))
	return "Failed to list spaces"
}

// memoStatus describes a memo's visibility for confirmations, naming its
// space when the title is known. The result is Markdown.
func memoStatus(memo *memosapi.Memo, spaceTitle string) string {
	status := string(memo.Visibility)
	if memo.Space != "" && spaceTitle != "" {
		status += " in " + escapeMarkdown(spaceTitle)
	}
	return status
}

// handleSpace shows the sender's default space for new memos and offers the
// spaces they can choose instead.
func (s *Service) handleSpace(ctx context.Context, _ *bot.Bot, update *models.Update) {
	msg := update.Message
	accessToken, ok := s.store.AccessToken(msg.From.ID)
	if !ok {
		s.reply(ctx, msg.Chat.ID, startFirst)
		return
	}
	if !s.spaces.Load() {
		s.reply(ctx, msg.Chat.ID, spacesUnsupported)
		return
	}
	spaces, err := s.listSpaces(ctx, s.memos.WithAccessToken(accessToken))
	if err != nil {
		s.reply(ctx, msg.Chat.ID, spacesErrorText(err))
		return
	}
	current := s.store.DefaultSpace(msg.From.ID)
	if len(spaces) == 0 && current.Name == "" {
		s.reply(ctx, msg.Chat.ID, noSpaces)
		return
	}
	text := "New memos are private. Choose a default space for them:"
	if current.Name != "" {
		text = fmt.Sprintf("New memos are saved in %s. Choose another default space:", storedTitle(current))
	}
	s.send(ctx, &bot.SendMessageParams{
		ChatID: msg.Chat.ID,
		Text:   text,
		ReplyMarkup: spacePicker(spaces, current.Name,
			func(key string) callbackData { return callbackData{action: actionDefault, space: key} },
			dataButton("No space", callbackData{action: actionDefault, space: noSpaceKey}),
		),
	})
}

// chooseDefaultSpace saves the default space picked from /space and replaces
// the picker with the outcome.
func (s *Service) chooseDefaultSpace(ctx context.Context, query *models.CallbackQuery, client *memos.Client, key string) {
	var space store.Space
	if key != noSpaceKey {
		spaces, err := s.listSpaces(ctx, client)
		if err != nil {
			s.alertSpacesError(ctx, query.ID, err)
			return
		}
		found, ok := findSpace(spaces, key)
		if !ok {
			s.alert(ctx, query.ID, "Spaces changed, send /space again")
			return
		}
		space = store.Space{Name: found.Name, Title: spaceTitle(found)}
	}
	if err := s.store.SetDefaultSpace(query.From.ID, space); err != nil {
		slog.Error("failed to save default space", slog.Any("err", err))
		s.alert(ctx, query.ID, "Failed to save default space")
		return
	}
	text := "New memos are private."
	if space.Name != "" {
		text = "New memos will be saved in " + space.Title + "."
	}
	// Editing without a keyboard removes the picker.
	if msg := query.Message.Message; msg != nil {
		s.edit(ctx, &bot.EditMessageTextParams{ChatID: msg.Chat.ID, MessageID: msg.ID, Text: text})
	}
	s.answer(ctx, query.ID, "Default space updated")
}

func storedTitle(space store.Space) string {
	return cmp.Or(space.Title, space.Name)
}

// createInDefaultSpace creates a memo in the user's default space, if any.
// When the server rejects that space, because it was deleted or the user left
// it, the memo is created without it, the default is cleared, and the lost
// space is returned so that the user can be told.
func (s *Service) createInDefaultSpace(ctx context.Context, client *memos.Client, userID int64, content string) (*memosapi.Memo, store.Space, error) {
	space := s.store.DefaultSpace(userID)
	if space.Name == "" || !s.spaces.Load() {
		memo, err := client.CreateMemo(ctx, memosapi.Memo{Content: content})
		return memo, store.Space{}, err
	}
	memo, err := client.CreateMemo(ctx, memosapi.Memo{Content: content, Space: space.Name, Visibility: memosapi.SPACE})
	if !memos.RejectsSpace(err) {
		return memo, store.Space{}, err
	}
	slog.Warn("default space rejected, saving the memo without it", slog.String("space", space.Name), slog.Any("err", err))
	memo, err = client.CreateMemo(ctx, memosapi.Memo{Content: content})
	if err != nil {
		return nil, store.Space{}, err
	}
	// Clear the default only now: the retry shows that the space, not the
	// content, was rejected.
	if err := s.store.SetDefaultSpace(userID, store.Space{}); err != nil {
		slog.Error("failed to clear default space", slog.Any("err", err))
	}
	return memo, space, nil
}
