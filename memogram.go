package memogram

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/usememos/memogram/internal/memosapi"
	"github.com/usememos/memogram/store"
)

type Service struct {
	bot        *bot.Bot
	client     *MemosClient
	config     *Config
	store      *store.Store
	httpClient *http.Client

	mediaGroupCache sync.Map
	mediaGroupMutex sync.Mutex

	instanceProfile  *memosapi.InstanceProfile
	allowedUsernames map[string]struct{}
}

const (
	commandStart  = "/start"
	commandSearch = "/search"
)

func NewService() (*Service, error) {
	config, err := getConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to get config from env: %w", err)
	}

	client := NewMemosClient(config.ServerAddr)

	store := store.NewStore(config.Data)
	if err := store.Init(); err != nil {
		return nil, fmt.Errorf("failed to init store: %w", err)
	}

	allowedUsernames := parseAllowedUsernames(config.AllowedUsernames)
	s := &Service{
		config:           config,
		client:           client,
		store:            store,
		httpClient:       &http.Client{Timeout: 60 * time.Second},
		allowedUsernames: allowedUsernames,
	}

	opts := []bot.Option{
		bot.WithDefaultHandler(s.handler),
		bot.WithCallbackQueryDataHandler("", bot.MatchTypePrefix, s.callbackQueryHandler),
	}
	if config.BotProxyAddr != "" {
		opts = append(opts, bot.WithServerURL(config.BotProxyAddr))
	}

	b, err := bot.New(config.BotToken, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}
	s.bot = b

	return s, nil
}

func (s *Service) Start(ctx context.Context) {
	slog.Info("Memogram started")
	// Select the API base path before polling Telegram, including when Memos
	// starts later than Memogram in a Compose deployment.
	for {
		profile, err := s.client.GetInstanceProfile(ctx)
		if err == nil {
			slog.Info("instance profile", slog.String("version", profile.Version))
			s.instanceProfile = profile
			break
		}
		slog.Warn("waiting for Memos instance profile", slog.Any("err", err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}

	// set bot commands
	commands := []models.BotCommand{
		{
			Command:     "start",
			Description: "Start the bot with access token",
		},
		{
			Command:     "search",
			Description: "Search for the memos",
		},
	}
	_, err := s.bot.SetMyCommands(ctx, &bot.SetMyCommandsParams{Commands: commands})
	if err != nil {
		slog.Error("failed to set bot commands", slog.Any("err", err))
	}

	s.bot.Start(ctx)
}

func (s *Service) createMemo(ctx context.Context, client *MemosClient, content string) (*memosapi.Memo, error) {
	memo, err := client.CreateMemo(ctx, content)
	if err != nil {
		slog.Error("failed to create memo", slog.Any("err", err))
		return nil, fmt.Errorf("create memo: %w", err)
	}
	return memo, nil
}

func (s *Service) handleMemoCreation(ctx context.Context, client *MemosClient, m *models.Update, content string) (*memosapi.Memo, error) {
	var memo *memosapi.Memo
	var err error

	if m.Message.MediaGroupID != "" {
		s.mediaGroupMutex.Lock()
		defer s.mediaGroupMutex.Unlock()

		if cache, ok := s.mediaGroupCache.Load(m.Message.MediaGroupID); ok {
			return cache.(*memosapi.Memo), nil
		}

		memo, err = s.createMemo(ctx, client, content)
		if err != nil {
			return nil, err
		}
		s.mediaGroupCache.Store(m.Message.MediaGroupID, memo)
	} else {
		memo, err = s.createMemo(ctx, client, content)
		if err != nil {
			return nil, err
		}
	}

	return memo, nil
}

func (s *Service) handler(ctx context.Context, b *bot.Bot, m *models.Update) {
	// Defensive nil checks
	if s == nil || s.config == nil {
		fmt.Println("Service or config is nil")
		return
	}
	if m == nil || m.Message == nil || m.Message.From == nil {
		s.sendError(b, 0, errors.New("invalid message structure: missing required fields"))
		return
	}
	if m.Message.Chat.ID == 0 {
		s.sendError(b, 0, errors.New("invalid chat: missing chat ID"))
		return
	}

	username := m.Message.From.Username
	if !s.isUserAllowed(username) {
		if username == "" {
			s.sendError(b, m.Message.Chat.ID, errors.New("your account must have a username to use this bot"))
			return
		}
		s.sendError(b, m.Message.Chat.ID, fmt.Errorf("your account %s is not allowed to use this bot", username))
		return
	}

	message := m.Message
	if strings.HasPrefix(message.Text, commandStart+" ") || message.Text == commandStart {
		s.startHandler(ctx, b, m)
		return
	} else if strings.HasPrefix(message.Text, commandSearch+" ") || message.Text == commandSearch {
		s.searchHandler(ctx, b, m)
		return
	}

	userID := message.From.ID
	if _, ok := s.store.GetUserAccessToken(userID); !ok {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: message.Chat.ID,
			Text:   "Please start the bot with /start <access_token>",
		})
		return
	}

	content := message.Text
	contentEntities := message.Entities
	if message.Caption != "" {
		content = message.Caption
		contentEntities = message.CaptionEntities
	}
	if len(contentEntities) > 0 {
		content = formatContent(content, contentEntities)
	}

	// Add "forwarded from: originName" if message was forwarded
	if message.ForwardOrigin != nil {
		var originName, originUsername string
		// Determine the type of origin
		switch origin := message.ForwardOrigin; {
		case origin.MessageOriginUser != nil: // User
			user := origin.MessageOriginUser.SenderUser
			if user.LastName != "" {
				originName = fmt.Sprintf("%s %s", user.FirstName, user.LastName)
			} else {
				originName = user.FirstName
			}
			originUsername = user.Username
		case origin.MessageOriginHiddenUser != nil: // Hidden User
			hiddenUserName := origin.MessageOriginHiddenUser.SenderUserName
			if hiddenUserName != "" {
				originName = hiddenUserName
			} else {
				originName = "Hidden User"
			}
		case origin.MessageOriginChat != nil: // Chat
			chat := origin.MessageOriginChat.SenderChat
			originName = chat.Title
			originUsername = chat.Username
		case origin.MessageOriginChannel != nil: // Channel
			channel := origin.MessageOriginChannel.Chat
			originName = channel.Title
			originUsername = channel.Username
		}

		if originUsername != "" {
			content = fmt.Sprintf("Forwarded from [%s](https://t.me/%s)\n%s", originName, originUsername, content)
		} else {
			content = fmt.Sprintf("Forwarded from %s\n%s", originName, content)
		}
	}

	hasAttachment := message.Document != nil || len(message.Photo) > 0 || message.Voice != nil || message.Video != nil
	if content == "" && !hasAttachment {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: message.Chat.ID,
			Text:   "Please input memo content",
		})
		return
	}

	accessToken, _ := s.store.GetUserAccessToken(userID)
	authClient := s.client.NewAuthenticatedClient(accessToken)

	var memo *memosapi.Memo
	memo, err := s.handleMemoCreation(ctx, authClient, m, content)
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: message.Chat.ID,
			Text:   "Failed to create memo",
		})
		return
	}

	var fileID string
	switch {
	case message.Document != nil:
		fileID = message.Document.FileID
	case message.Voice != nil:
		fileID = message.Voice.FileID
	case message.Video != nil:
		fileID = message.Video.FileID
	case len(message.Photo) > 0:
		fileID = message.Photo[len(message.Photo)-1].FileID
	}
	attachmentFailed := false
	if fileID != "" {
		if err := s.processFileMessage(ctx, authClient, b, fileID, memo); err != nil {
			attachmentFailed = true
			slog.Error("attachment upload failed", slog.Any("err", err))
		}
	}

	memoUID, err := ExtractMemoUIDFromName(memo.Name)
	if err != nil {
		slog.Error("failed to extract memo UID", slog.Any("err", err))
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: message.Chat.ID,
			Text:   "Failed to save memo",
		})
		return
	}

	text := fmt.Sprintf("Content saved as %s with [%s](%s/memos/%s)", memo.Visibility, memo.Name, s.memoBaseURL(), memoUID)
	if attachmentFailed {
		text = fmt.Sprintf("Memo saved, but the attachment failed to upload. [%s](%s/memos/%s)", memo.Name, s.memoBaseURL(), memoUID)
	}
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:              message.Chat.ID,
		Text:                text,
		ParseMode:           models.ParseModeMarkdown,
		DisableNotification: true,
		ReplyParameters: &models.ReplyParameters{
			MessageID: message.ID,
		},
		ReplyMarkup: s.keyboard(memo),
	})
}

func (s *Service) startHandler(ctx context.Context, b *bot.Bot, m *models.Update) {
	userID := m.Message.From.ID
	accessToken := strings.TrimSpace(strings.TrimPrefix(m.Message.Text, commandStart))
	if accessToken == "" {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
			Text:   "Usage: /start <access_token>",
		})
		return
	}

	authClient := s.client.NewAuthenticatedClient(accessToken)
	user, err := authClient.GetCurrentUser(ctx)
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
			Text:   "Invalid access token",
		})
		return
	}
	s.store.SetUserAccessToken(userID, accessToken)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: m.Message.Chat.ID,
		Text:   fmt.Sprintf("Hello %s!", user.DisplayName),
	})
}

func (s *Service) keyboard(memo *memosapi.Memo) *models.InlineKeyboardMarkup {
	// add inline keyboard to edit memo's visibility or pinned status.
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{
					Text:         "Public",
					CallbackData: fmt.Sprintf("public %s", memo.Name),
				},
				{
					Text:         "Private",
					CallbackData: fmt.Sprintf("private %s", memo.Name),
				},
				{
					Text:         "Pin",
					CallbackData: fmt.Sprintf("pin %s", memo.Name),
				},
			},
		},
	}
}

func (s *Service) callbackQueryHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	callbackData := update.CallbackQuery.Data
	userID := update.CallbackQuery.From.ID
	accessToken, ok := s.store.GetUserAccessToken(userID)
	if !ok {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            "Please start the bot with /start <access_token>",
			ShowAlert:       true,
		})
		return
	}

	authClient := s.client.NewAuthenticatedClient(accessToken)

	parts := strings.Split(callbackData, " ")
	if len(parts) != 2 {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            "Invalid command",
			ShowAlert:       true,
		})
		return
	}
	slog.Info("parts", slog.Any("parts", parts))
	action, memoName := parts[0], parts[1]

	memo, err := authClient.GetMemo(ctx, memoName)
	if err != nil {
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            fmt.Sprintf("Memo %s not found", memoName),
			ShowAlert:       true,
		})
		return
	}

	switch action {
	case "public":
		memo.Visibility = memosapi.PUBLIC
	case "protected":
		memo.Visibility = memosapi.PROTECTED
	case "private":
		memo.Visibility = memosapi.PRIVATE
	case "pin":
		memo.Pinned = !memo.Pinned
	default:
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            "Unknown action",
			ShowAlert:       true,
		})
		return
	}

	_, e := authClient.UpdateMemo(ctx, memo)
	if e != nil {
		slog.Error("failed to update memo", slog.Any("err", e))
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            "Failed to update memo",
			ShowAlert:       true,
		})
		return
	}
	var pinnedMarker string
	if memo.Pinned {
		pinnedMarker = "📌"
	} else {
		pinnedMarker = ""
	}

	memoUID, err := ExtractMemoUIDFromName(memo.Name)
	if err != nil {
		slog.Error("failed to extract memo UID", slog.Any("err", err))
		b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            "Failed to update memo",
		})
		return
	}
	b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:      update.CallbackQuery.Message.Message.Chat.ID,
		MessageID:   update.CallbackQuery.Message.Message.ID,
		Text:        fmt.Sprintf("Memo updated as %s with [%s](%s/memos/%s) %s", memo.Visibility, memo.Name, s.memoBaseURL(), memoUID, pinnedMarker),
		ParseMode:   models.ParseModeMarkdown,
		ReplyMarkup: s.keyboard(memo),
	})

	b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
		Text:            "Memo updated",
	})
}

func (s *Service) searchHandler(ctx context.Context, b *bot.Bot, m *models.Update) {
	userID := m.Message.From.ID
	searchString := strings.TrimSpace(strings.TrimPrefix(m.Message.Text, commandSearch))
	if searchString == "" {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
			Text:   "Usage: /search <words>",
		})
		return
	}
	accessToken, ok := s.store.GetUserAccessToken(userID)
	if !ok {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
			Text:   "Please start the bot with /start <access_token>",
		})
		return
	}
	authClient := s.client.NewAuthenticatedClient(accessToken)
	user, err := authClient.GetCurrentUser(ctx)
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
			Text:   "Invalid access token",
		})
		return
	}
	filter := buildMemoSearchFilter(searchString, user)
	memos, err := authClient.ListMemos(ctx, filter)
	if err != nil {
		slog.Error("failed to search memos", slog.Any("err", err))
		return
	}

	if len(memos) == 0 {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
			Text:   "No memos found for the specified search criteria.",
		})
	} else {
		for _, memo := range memos {
			tgMessage := memo.Name + "\n" + memo.Content
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: m.Message.Chat.ID,
				Text:   tgMessage,
			})
		}
	}
}

func buildMemoSearchFilter(searchString string, user *memosapi.User) string {
	filter := fmt.Sprintf("content.contains(%q)", searchString)
	if user == nil {
		return filter
	}

	creator := user.Name
	if creator == "" && user.Username != "" {
		creator = "users/" + user.Username
	}
	if creator == "" {
		return filter
	}

	return fmt.Sprintf("%s && creator == %q", filter, creator)
}

func (s *Service) processFileMessage(ctx context.Context, client *MemosClient, b *bot.Bot, fileID string, memo *memosapi.Memo) error {
	file, err := b.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		return fmt.Errorf("get Telegram file: %w", err)
	}

	_, err = s.saveAttachmentFromFile(ctx, client, file, memo)
	return err
}

const maxDownloadBytes = 20 * 1024 * 1024

func (s *Service) saveAttachmentFromFile(ctx context.Context, client *MemosClient, file *models.File, memo *memosapi.Memo) (*memosapi.Attachment, error) {
	if file.FileSize > maxDownloadBytes {
		return nil, fmt.Errorf("Telegram file exceeds the 20 MiB download limit")
	}
	fileLink := s.bot.FileDownloadLink(file)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileLink, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	response, err := s.httpClient.Do(req)
	if err != nil {
		// Download URLs contain the bot token; keep them out of error logs.
		var downloadErr *url.Error
		if errors.As(err, &downloadErr) {
			err = downloadErr.Err
		}
		return nil, fmt.Errorf("failed to download file: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("download failed with status %s", response.Status)
	}

	bytes, err := io.ReadAll(io.LimitReader(response.Body, maxDownloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	if len(bytes) > maxDownloadBytes {
		return nil, fmt.Errorf("Telegram file exceeds the 20 MiB download limit")
	}

	contentType := response.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(bytes)
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	attachment, err := client.CreateAttachment(ctx, memosapi.Attachment{
		Filename: filepath.Base(file.FilePath), Type: contentType,
		Content: base64.StdEncoding.EncodeToString(bytes), Memo: memo.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create attachment: %w", err)
	}

	return attachment, nil
}

func (s *Service) memoBaseURL() string {
	if s.instanceProfile != nil && s.instanceProfile.InstanceUrl != "" {
		if baseURL, err := normalizeServerAddr(s.instanceProfile.InstanceUrl); err == nil {
			return baseURL
		}
	}
	return s.client.baseURL
}

func (s *Service) sendError(b *bot.Bot, chatID int64, err error) {
	slog.Error("error", slog.Any("err", err))
	b.SendMessage(context.Background(), &bot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("Error: %s", err.Error()),
	})
}

func parseAllowedUsernames(raw string) map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, entry := range strings.Split(raw, ",") {
		trimmed := strings.ToLower(strings.TrimSpace(entry))
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}
	return allowed
}

func (s *Service) isUserAllowed(username string) bool {
	if len(s.allowedUsernames) == 0 {
		return true
	}
	if username == "" {
		return false
	}
	_, ok := s.allowedUsernames[strings.ToLower(strings.TrimSpace(username))]
	return ok
}

func formatContent(content string, contentEntities []models.MessageEntity) string {
	sort.Slice(contentEntities, func(i, j int) bool {
		if contentEntities[i].Offset == contentEntities[j].Offset {
			return contentEntities[i].Length < contentEntities[j].Length
		}
		return contentEntities[i].Offset < contentEntities[j].Offset
	})

	contentRunes := utf16.Encode([]rune(content))

	var sb strings.Builder
	cursor := 0
	for _, entity := range contentEntities {
		if !isSupportedEntity(entity.Type) {
			continue
		}
		start := entity.Offset
		end := entity.Offset + entity.Length
		if start < cursor {
			// Ignore overlapping entities to avoid double formatting.
			continue
		}
		if start >= len(contentRunes) {
			break
		}
		if end > len(contentRunes) {
			end = len(contentRunes)
		}

		sb.WriteString(string(utf16.Decode(contentRunes[cursor:start])))
		segment := string(utf16.Decode(contentRunes[start:end]))
		sb.WriteString(applyEntityFormatting(segment, entity))
		cursor = end
	}
	sb.WriteString(string(utf16.Decode(contentRunes[cursor:])))
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

func applyEntityFormatting(segment string, entity models.MessageEntity) string {
	if strings.TrimSpace(segment) == "" {
		return segment
	}
	re := regexp.MustCompile(`^(\s*)(.*?)(\s*)$`)
	matches := re.FindStringSubmatch(segment)
	if len(matches) != 4 {
		return segment
	}
	prefix, core, suffix := matches[1], matches[2], matches[3]
	switch entity.Type {
	case models.MessageEntityTypeURL:
		return fmt.Sprintf("%s[%s](%s)%s", prefix, core, core, suffix)
	case models.MessageEntityTypeTextLink:
		return fmt.Sprintf("%s[%s](%s)%s", prefix, core, entity.URL, suffix)
	case models.MessageEntityTypeBold:
		return fmt.Sprintf("%s**%s**%s", prefix, core, suffix)
	case models.MessageEntityTypeItalic:
		return fmt.Sprintf("%s*%s*%s", prefix, core, suffix)
	default:
		return segment
	}
}
