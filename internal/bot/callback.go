package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memos"
	"github.com/usememos/memogram/internal/memosapi"
)

// memoAction is what a button does.
type memoAction string

const (
	actionPublic    memoAction = "public"
	actionProtected memoAction = "protected" // accepted, but has no button
	actionPrivate   memoAction = "private"
	actionPin       memoAction = "pin" // toggles
	actionSpace     memoAction = "space"
	actionBack      memoAction = "back"

	// Choices in a space picker, which carry a space key.
	actionPlace   memoAction = "sp" // puts a memo in a space
	actionDefault memoAction = "sd" // sets the default space for new memos
)

func (a memoAction) valid() bool {
	switch a {
	case actionPublic, actionProtected, actionPrivate, actionPin, actionSpace, actionBack, actionPlace, actionDefault:
		return true
	default:
		return false
	}
}

func (a memoAction) needsSpaces() bool {
	return a == actionSpace || a == actionPlace || a == actionDefault
}

// apply changes memo as a visibility or pin action describes and returns the
// changed field.
func (a memoAction) apply(memo *memosapi.Memo) string {
	switch a {
	case actionPublic:
		memo.Visibility = memosapi.PUBLIC
	case actionProtected:
		memo.Visibility = memosapi.PROTECTED
	case actionPrivate:
		memo.Visibility = memosapi.PRIVATE
	case actionPin:
		memo.Pinned = !memo.Pinned
		return memos.FieldPinned
	}
	return memos.FieldVisibility
}

// callbackData is the payload of a button. Telegram limits it to 64 bytes.
//
//	<action> <memo name>         memo actions, including "space" and "back"
//	sp <memo name> <space key>   put a memo in a space, or "-" for none
//	sd <space key>               set the default space, or "-" for none
//
// Buttons on messages sent by earlier versions use the first form, so it must
// stay compatible.
type callbackData struct {
	action memoAction
	memo   string
	space  string
}

var errInvalidCallback = errors.New("invalid callback data")

func (d callbackData) String() string {
	switch d.action {
	case actionPlace:
		return string(d.action) + " " + d.memo + " " + d.space
	case actionDefault:
		return string(d.action) + " " + d.space
	default:
		return string(d.action) + " " + d.memo
	}
}

func parseCallbackData(data string) (callbackData, error) {
	parts := strings.Split(data, " ")
	action := memoAction(parts[0])
	switch {
	case action == actionPlace && len(parts) == 3:
		return callbackData{action: action, memo: parts[1], space: parts[2]}, nil
	case action == actionDefault && len(parts) == 2:
		return callbackData{action: action, space: parts[1]}, nil
	case action != actionPlace && action != actionDefault && len(parts) == 2:
		return callbackData{action: action, memo: parts[1]}, nil
	default:
		return callbackData{}, errInvalidCallback
	}
}

func dataButton(text string, data callbackData) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{Text: text, CallbackData: data.String()}
}

// memoKeyboard returns the buttons shown under a saved or updated memo.
func (s *Service) memoKeyboard(memo *memosapi.Memo) *models.InlineKeyboardMarkup {
	button := func(text string, action memoAction) models.InlineKeyboardButton {
		return dataButton(text, callbackData{action: action, memo: memo.Name})
	}
	rows := [][]models.InlineKeyboardButton{{
		button("Public", actionPublic),
		button("Private", actionPrivate),
		button("Pin", actionPin),
	}}
	if s.spaces.Load() {
		rows = append(rows, []models.InlineKeyboardButton{button("Space", actionSpace)})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// handleCallback handles a button press. Memo buttons update the memo, then
// the message they belong to with the memo's new state.
func (s *Service) handleCallback(ctx context.Context, _ *bot.Bot, update *models.Update) {
	query := update.CallbackQuery
	accessToken, ok := s.store.AccessToken(query.From.ID)
	if !ok {
		s.alert(ctx, query.ID, startFirst)
		return
	}
	data, err := parseCallbackData(query.Data)
	if err != nil {
		s.alert(ctx, query.ID, "Invalid command")
		return
	}
	if !data.action.valid() {
		s.alert(ctx, query.ID, "Unknown action")
		return
	}
	if data.action.needsSpaces() && !s.spaces.Load() {
		s.alert(ctx, query.ID, spacesUnsupported)
		return
	}

	client := s.memos.WithAccessToken(accessToken)
	switch data.action {
	case actionDefault:
		s.chooseDefaultSpace(ctx, query, client, data.space)
		return
	case actionBack:
		s.restoreKeyboard(ctx, query, data.memo)
		return
	}
	memo, err := client.GetMemo(ctx, data.memo)
	if err != nil {
		s.alert(ctx, query.ID, fmt.Sprintf("Memo %s not found", data.memo))
		return
	}
	switch data.action {
	case actionSpace:
		s.openSpacePicker(ctx, query, client, memo)
	case actionPlace:
		s.placeMemo(ctx, query, client, memo, data.space)
	default:
		field := data.action.apply(memo)
		if _, err := client.UpdateMemo(ctx, memo, field); err != nil {
			slog.Error("failed to update memo", slog.Any("err", err))
			s.alert(ctx, query.ID, "Failed to update memo")
			return
		}
		s.confirmUpdate(ctx, query, memo, "")
	}
}

// confirmUpdate shows a memo's new state in the message whose button was
// pressed. spaceTitle names the memo's space, when known.
func (s *Service) confirmUpdate(ctx context.Context, query *models.CallbackQuery, memo *memosapi.Memo, spaceTitle string) {
	link, err := s.memoLink(memo)
	if err != nil {
		slog.Error("failed to link memo", slog.Any("err", err))
		s.answer(ctx, query.ID, "Failed to update memo")
		return
	}
	// The message is inaccessible when it is too old or was deleted. The
	// update still succeeded, so only the confirmation is skipped.
	if msg := query.Message.Message; msg != nil {
		text := fmt.Sprintf("Memo updated as %s with %s", memoStatus(memo, spaceTitle), link)
		if memo.Pinned {
			text += " 📌"
		}
		s.edit(ctx, &bot.EditMessageTextParams{
			ChatID:      msg.Chat.ID,
			MessageID:   msg.ID,
			Text:        text,
			ParseMode:   models.ParseModeMarkdown,
			ReplyMarkup: s.memoKeyboard(memo),
		})
	}
	s.answer(ctx, query.ID, "Memo updated")
}

// openSpacePicker replaces a memo's buttons with the user's spaces.
func (s *Service) openSpacePicker(ctx context.Context, query *models.CallbackQuery, client *memos.Client, memo *memosapi.Memo) {
	spaces, err := s.listSpaces(ctx, client)
	if err != nil {
		s.alertSpacesError(ctx, query.ID, err)
		return
	}
	if len(spaces) == 0 && memo.Space == "" {
		s.alert(ctx, query.ID, noSpaces)
		return
	}
	msg := query.Message.Message
	if msg == nil {
		s.alert(ctx, query.ID, "This message can no longer be changed")
		return
	}
	keyboard := spacePicker(spaces, memo.Space,
		func(key string) callbackData { return callbackData{action: actionPlace, memo: memo.Name, space: key} },
		dataButton("No space", callbackData{action: actionPlace, memo: memo.Name, space: noSpaceKey}),
		dataButton("Back", callbackData{action: actionBack, memo: memo.Name}),
	)
	s.editMarkup(ctx, msg, keyboard)
	s.answer(ctx, query.ID, "Choose a space")
}

// placeMemo moves a memo into the chosen space, with SPACE visibility so that
// members can see it, or takes it out of its space.
func (s *Service) placeMemo(ctx context.Context, query *models.CallbackQuery, client *memos.Client, memo *memosapi.Memo, key string) {
	var title string
	if key == noSpaceKey {
		memo.Space = ""
		if memo.Visibility == memosapi.SPACE {
			memo.Visibility = memosapi.PRIVATE
		}
	} else {
		spaces, err := s.listSpaces(ctx, client)
		if err != nil {
			s.alertSpacesError(ctx, query.ID, err)
			return
		}
		space, ok := findSpace(spaces, key)
		if !ok {
			s.alert(ctx, query.ID, "Spaces changed, tap Space again")
			return
		}
		memo.Space, memo.Visibility, title = space.Name, memosapi.SPACE, spaceTitle(space)
	}
	if _, err := client.UpdateMemo(ctx, memo, memos.FieldSpace, memos.FieldVisibility); err != nil {
		slog.Error("failed to update memo space", slog.Any("err", err))
		s.alert(ctx, query.ID, "Failed to update memo")
		return
	}
	s.confirmUpdate(ctx, query, memo, title)
}

// restoreKeyboard closes a space picker without changing the memo.
func (s *Service) restoreKeyboard(ctx context.Context, query *models.CallbackQuery, name string) {
	if _, err := memos.MemoUID(name); err != nil {
		s.alert(ctx, query.ID, "Invalid command")
		return
	}
	if msg := query.Message.Message; msg != nil {
		s.editMarkup(ctx, msg, s.memoKeyboard(&memosapi.Memo{Name: name}))
	}
	s.answer(ctx, query.ID, "")
}
