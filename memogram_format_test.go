package memogram

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
