package bot

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/go-telegram/bot/models"
)

// messageContent returns the memo text for a message: its text or caption,
// with supported formatting converted to Markdown, after a line naming the
// original sender if it was forwarded.
func messageContent(msg *models.Message) string {
	content, entities := msg.Text, msg.Entities
	if msg.Caption != "" {
		content, entities = msg.Caption, msg.CaptionEntities
	}
	if len(entities) > 0 {
		content = formatContent(content, entities)
	}
	if msg.ForwardOrigin != nil {
		content = forwardHeader(msg.ForwardOrigin) + "\n" + content
	}
	return content
}

// forwardHeader names the original sender of a forwarded message, linking to
// it when it has a public username.
func forwardHeader(origin *models.MessageOrigin) string {
	var name, username string
	switch {
	case origin.MessageOriginUser != nil:
		user := origin.MessageOriginUser.SenderUser
		name = strings.TrimSpace(user.FirstName + " " + user.LastName)
		username = user.Username
	case origin.MessageOriginHiddenUser != nil:
		name = origin.MessageOriginHiddenUser.SenderUserName
		if name == "" {
			name = "Hidden User"
		}
	case origin.MessageOriginChat != nil:
		name, username = origin.MessageOriginChat.SenderChat.Title, origin.MessageOriginChat.SenderChat.Username
	case origin.MessageOriginChannel != nil:
		name, username = origin.MessageOriginChannel.Chat.Title, origin.MessageOriginChannel.Chat.Username
	}
	if name == "" {
		name = "an unknown sender"
	}
	if username != "" {
		return fmt.Sprintf("Forwarded from [%s](https://t.me/%s)", name, username)
	}
	return "Forwarded from " + name
}

// formatContent converts Telegram entities to Markdown. Entity offsets count
// UTF-16 code units. Entities that overlap an earlier one are skipped, so
// nested formatting keeps only the outermost style.
func formatContent(content string, entities []models.MessageEntity) string {
	entities = slices.Clone(entities)
	slices.SortStableFunc(entities, func(a, b models.MessageEntity) int {
		return cmp.Or(cmp.Compare(a.Offset, b.Offset), cmp.Compare(a.Length, b.Length))
	})

	units := utf16.Encode([]rune(content))
	var sb strings.Builder
	cursor := 0
	for _, entity := range entities {
		if !isSupportedEntity(entity.Type) {
			continue
		}
		start, end := entity.Offset, min(entity.Offset+entity.Length, len(units))
		if start < cursor {
			continue
		}
		if start >= len(units) {
			break
		}
		sb.WriteString(string(utf16.Decode(units[cursor:start])))
		sb.WriteString(formatSegment(string(utf16.Decode(units[start:end])), entity))
		cursor = end
	}
	sb.WriteString(string(utf16.Decode(units[cursor:])))
	return sb.String()
}

func isSupportedEntity(entityType models.MessageEntityType) bool {
	switch entityType {
	case models.MessageEntityTypeURL,
		models.MessageEntityTypeTextLink,
		models.MessageEntityTypeBold,
		models.MessageEntityTypeItalic:
		return true
	default:
		return false
	}
}

// formatSegment formats each line of an entity separately. Markdown markers
// cannot span a blank line, and whitespace must stay outside them.
func formatSegment(segment string, entity models.MessageEntity) string {
	lines := strings.Split(segment, "\n")
	for i, line := range lines {
		lines[i] = formatLine(line, entity)
	}
	return strings.Join(lines, "\n")
}

func formatLine(line string, entity models.MessageEntity) string {
	core := strings.TrimSpace(line)
	if core == "" {
		return line
	}
	start := strings.Index(line, core)
	prefix, suffix := line[:start], line[start+len(core):]
	switch entity.Type {
	case models.MessageEntityTypeURL:
		core = fmt.Sprintf("[%s](%s)", core, core)
	case models.MessageEntityTypeTextLink:
		core = fmt.Sprintf("[%s](%s)", core, entity.URL)
	case models.MessageEntityTypeBold:
		core = "**" + core + "**"
	case models.MessageEntityTypeItalic:
		core = "*" + core + "*"
	}
	return prefix + core + suffix
}
