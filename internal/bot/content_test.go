package bot

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestFormatContent_MixedEntities(t *testing.T) {
	content := "See example.com and bold text link"
	entities := []models.MessageEntity{
		{
			Type:   models.MessageEntityTypeURL,
			Offset: 4,
			Length: 11,
		},
		{
			Type:   models.MessageEntityTypeBold,
			Offset: 20,
			Length: 4,
		},
		{
			Type:   models.MessageEntityTypeTextLink,
			Offset: 30,
			Length: 4,
			URL:    "https://example.com",
		},
	}

	got := formatContent(content, entities)
	want := "See [example.com](example.com) and **bold** text [link](https://example.com)"
	if got != want {
		t.Fatalf("unexpected content:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatContent_OutOfOrderEntities(t *testing.T) {
	content := "Italic and bold"
	entities := []models.MessageEntity{
		{
			Type:   models.MessageEntityTypeBold,
			Offset: 11,
			Length: 4,
		},
		{
			Type:   models.MessageEntityTypeItalic,
			Offset: 0,
			Length: 6,
		},
	}

	got := formatContent(content, entities)
	want := "*Italic* and **bold**"
	if got != want {
		t.Fatalf("unexpected content:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatContent_OverlappingEntities(t *testing.T) {
	content := "Overlap test"
	entities := []models.MessageEntity{
		{
			Type:   models.MessageEntityTypeBold,
			Offset: 0,
			Length: 7,
		},
		{
			Type:   models.MessageEntityTypeItalic,
			Offset: 5,
			Length: 4,
		},
	}

	got := formatContent(content, entities)
	want := "**Overlap** test"
	if got != want {
		t.Fatalf("unexpected content:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatContentMultilineEntities(t *testing.T) {
	content := "first line\n\n  second line \nplain"
	entities := []models.MessageEntity{{Type: models.MessageEntityTypeBold, Offset: 0, Length: 26}}
	got := formatContent(content, entities)
	want := "**first line**\n\n  **second line** \nplain"
	if got != want {
		t.Fatalf("unexpected content:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatContentCountsUTF16(t *testing.T) {
	// The emoji is two UTF-16 code units, so "bold" starts at offset 3.
	got := formatContent("🌱 bold", []models.MessageEntity{{Type: models.MessageEntityTypeBold, Offset: 3, Length: 4}})
	if want := "🌱 **bold**"; got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestFormatContentDoesNotReorderCallerEntities(t *testing.T) {
	entities := []models.MessageEntity{
		{Type: models.MessageEntityTypeBold, Offset: 4, Length: 1},
		{Type: models.MessageEntityTypeItalic, Offset: 0, Length: 1},
	}
	formatContent("a b c", entities)
	if entities[0].Offset != 4 {
		t.Fatal("formatContent sorted the caller's entities")
	}
}

func TestMessageContent(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  models.Message
		want string
	}{
		{"text", models.Message{Text: "hello"}, "hello"},
		{"caption wins", models.Message{Text: "ignored", Caption: "caption"}, "caption"},
		{"caption entities", models.Message{Caption: "a bold", CaptionEntities: []models.MessageEntity{{Type: models.MessageEntityTypeBold, Offset: 2, Length: 4}}}, "a **bold**"},
		{"forwarded user", models.Message{Text: "hi", ForwardOrigin: &models.MessageOrigin{MessageOriginUser: &models.MessageOriginUser{SenderUser: models.User{FirstName: "Ada", LastName: "Lovelace", Username: "ada"}}}}, "Forwarded from [Ada Lovelace](https://t.me/ada)\nhi"},
		{"forwarded user without last name", models.Message{Text: "hi", ForwardOrigin: &models.MessageOrigin{MessageOriginUser: &models.MessageOriginUser{SenderUser: models.User{FirstName: "Ada"}}}}, "Forwarded from Ada\nhi"},
		{"forwarded hidden user", models.Message{Text: "hi", ForwardOrigin: &models.MessageOrigin{MessageOriginHiddenUser: &models.MessageOriginHiddenUser{}}}, "Forwarded from Hidden User\nhi"},
		{"forwarded channel", models.Message{Text: "hi", ForwardOrigin: &models.MessageOrigin{MessageOriginChannel: &models.MessageOriginChannel{Chat: models.Chat{Title: "News", Username: "news"}}}}, "Forwarded from [News](https://t.me/news)\nhi"},
		{"forwarded chat", models.Message{Text: "hi", ForwardOrigin: &models.MessageOrigin{MessageOriginChat: &models.MessageOriginChat{SenderChat: models.Chat{Title: "Group"}}}}, "Forwarded from Group\nhi"},
		{"forwarded unknown origin", models.Message{Text: "hi", ForwardOrigin: &models.MessageOrigin{}}, "Forwarded from an unknown sender\nhi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := messageContent(&tc.msg); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}
