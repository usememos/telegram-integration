package bot

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestIgnoresUpdatesWithoutUserMessage(t *testing.T) {
	tg := newFakeTelegram(t, nil)
	s := newTestService(t, tg, Options{})
	for name, update := range map[string]*models.Update{
		"edited message": {EditedMessage: textMessage(1, "edited")},
		"channel post":   {Message: &models.Message{ID: 1, Chat: models.Chat{ID: -100}, Text: "post"}},
		"membership":     {MyChatMember: &models.ChatMemberUpdated{}},
	} {
		t.Run(name, func(t *testing.T) {
			s.process(update)
		})
	}
	if calls := tg.snapshot(); len(calls) != 0 {
		t.Fatalf("ignored updates produced Telegram calls: %+v", calls)
	}
}

func TestAllowedUsernames(t *testing.T) {
	tg := newFakeTelegram(t, nil)
	data := openStore(t)
	bindUser(t, data, 1, "token")
	s := newTestService(t, tg, Options{Store: data, AllowedUsernames: []string{"alice"}})
	message := func(username, text string) *models.Update {
		msg := textMessage(1, text)
		msg.From.Username = username
		return &models.Update{Message: msg}
	}
	for _, tc := range []struct {
		name   string
		update *models.Update
		want   telegramCall
	}{
		{"allowed, any case", message("Alice", "/start"), telegramCall{method: "sendMessage", chatID: 1, text: "Usage: /start <access_token>"}},
		{"denied message", message("mallory", "/start token"), telegramCall{method: "sendMessage", chatID: 1, text: "Error: your account mallory is not allowed to use this bot"}},
		{"no username", message("", "note"), telegramCall{method: "sendMessage", chatID: 1, text: "Error: your account must have a username to use this bot"}},
		{"denied button", &models.Update{CallbackQuery: &models.CallbackQuery{ID: "q", From: models.User{ID: 1, Username: "mallory"}, Data: "pin memos/abc"}}, telegramCall{method: "answerCallbackQuery", text: "Error: your account mallory is not allowed to use this bot", alert: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(tg.snapshot())
			s.process(tc.update)
			calls := tg.snapshot()[before:]
			if len(calls) != 1 || calls[0].method != tc.want.method || calls[0].chatID != tc.want.chatID || calls[0].text != tc.want.text || calls[0].alert != tc.want.alert {
				t.Fatalf("got %+v, want %+v", calls, tc.want)
			}
		})
	}
}
