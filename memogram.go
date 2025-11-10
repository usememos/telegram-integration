package memogram

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/pkg/errors"
	"github.com/usememos/memogram/store"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	fieldmaskpb "google.golang.org/protobuf/types/known/fieldmaskpb"
)

type Service struct {
	bot    *bot.Bot
	client *MemosClient
	config *Config
	store  *store.Store

	mediaGroupCache sync.Map
	mediaGroupMutex sync.Mutex

	workspaceProfile *v1pb.WorkspaceProfile
}

func NewService() (*Service, error) {
	config, err := getConfigFromEnv()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get config from env")
	}

	conn, err := grpc.NewClient(config.ServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("failed to connect to server", slog.Any("err", err))
		return nil, errors.Wrap(err, "failed to connect to server")
	}
	client := NewMemosClient(conn)

	store := store.NewStore(config.Data)
	if err := store.Init(); err != nil {
		return nil, errors.Wrap(err, "failed to init store")
	}
	s := &Service{
		config: config,
		client: client,
		store:  store,
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
		return nil, errors.Wrap(err, "failed to create bot")
	}
	s.bot = b

	return s, nil
}

func (s *Service) Start(ctx context.Context) {
	slog.Info("Memogram started")
	// Try to get workspace profile.
	workspaceProfile, err := s.client.WorkspaceService.GetWorkspaceProfile(ctx, &v1pb.GetWorkspaceProfileRequest{})
	if err != nil {
		slog.Error("failed to get workspace profile", slog.Any("err", err))
		return
	}
	slog.Info("workspace profile", slog.Any("profile", workspaceProfile))
	s.workspaceProfile = workspaceProfile

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
	_, err = s.bot.SetMyCommands(ctx, &bot.SetMyCommandsParams{Commands: commands})
	if err != nil {
		slog.Error("failed to set bot commands", slog.Any("err", err))
	}

	s.bot.Start(ctx)
}

func (s *Service) createMemo(ctx context.Context, content string) (*v1pb.Memo, error) {
	memo, err := s.client.MemoService.CreateMemo(ctx, &v1pb.CreateMemoRequest{
		Memo: &v1pb.Memo{
			Content: content,
		},
	})
	if err != nil {
		slog.Error("failed to create memo", slog.Any("err", err))
		return nil, err
	}
	return memo, nil
}

func (s *Service) handleMemoCreation(ctx context.Context, m *models.Update, content string) (*v1pb.Memo, error) {
	var memo *v1pb.Memo
	var err error

	if m.Message.MediaGroupID != "" {
		s.mediaGroupMutex.Lock()
		defer s.mediaGroupMutex.Unlock()

		if cache, ok := s.mediaGroupCache.Load(m.Message.MediaGroupID); ok {
			return cache.(*v1pb.Memo), nil
		}

		memo, err = s.createMemo(ctx, content)
		if err != nil {
			return nil, err
		}
		s.mediaGroupCache.Store(m.Message.MediaGroupID, memo)
	} else {
		memo, err = s.createMemo(ctx, content)
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

	// Check if user allowed users are specified
	if s.config.AllowedUsernames != "" {
		username := m.Message.From.Username
		if username == "" {
			s.sendError(b, m.Message.Chat.ID, errors.New("your account must have a username to use this bot"))
			return
		}
		allowedUsernames := strings.Split(s.config.AllowedUsernames, ",")
		for i := range allowedUsernames {
			allowedUsernames[i] = strings.TrimSpace(allowedUsernames[i])
		}
		username = strings.TrimSpace(username)
		contains := false
		for _, allowedUsername := range allowedUsernames {
			if allowedUsername == username {
				contains = true
				break
			}
		}
		if !contains {
			s.sendError(b, m.Message.Chat.ID, fmt.Errorf("your account %s is not allowed to use this bot", username))
			return
		}
	}
	if m.Message == nil {
		slog.Error("memo message is nil")
		return
	}
	message := m.Message
	if strings.HasPrefix(message.Text, "/start ") {
		s.startHandler(ctx, b, m)
		return
	} else if strings.HasPrefix(message.Text, "/search ") {
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
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("Authorization", fmt.Sprintf("Bearer %s", accessToken)))

	var memo *v1pb.Memo
	memo, err := s.handleMemoCreation(ctx, m, content)
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: message.Chat.ID,
			Text:   "Failed to create memo",
		})
		return
	}

	if message.Document != nil {
		s.processFileMessage(ctx, b, m, message.Document.FileID, memo)
	}
	if message.Voice != nil {
		s.processFileMessage(ctx, b, m, message.Voice.FileID, memo)
	}
	if message.Video != nil {
		s.processFileMessage(ctx, b, m, message.Video.FileID, memo)
	}
	if len(message.Photo) > 0 {
		photo := message.Photo[len(message.Photo)-1]
		s.processFileMessage(ctx, b, m, photo.FileID, memo)
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:              message.Chat.ID,
		Text:                fmt.Sprintf("Content saved as %s with [%s](%s/m/%s)", v1pb.Visibility_name[int32(memo.Visibility)], memo.Name, s.workspaceProfile.InstanceUrl, memoUID),
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
	accessToken := strings.TrimPrefix(m.Message.Text, "/start ")

	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("Authorization", fmt.Sprintf("Bearer %s", accessToken)))
	currentSessionResponse, err := s.client.AuthService.GetCurrentSession(ctx, &v1pb.GetCurrentSessionRequest{})
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
			Text:   "Invalid access token",
		})
		return
	}
	user := currentSessionResponse.User
	s.store.SetUserAccessToken(userID, accessToken)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: m.Message.Chat.ID,
		Text:   fmt.Sprintf("Hello %s!", user.DisplayName),
	})
}

func (s *Service) keyboard(memo *v1pb.Memo) *models.InlineKeyboardMarkup {
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

	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("Authorization", fmt.Sprintf("Bearer %s", accessToken)))

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

	memo, err := s.client.MemoService.GetMemo(ctx, &v1pb.GetMemoRequest{
		Name: memoName,
	})
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
		memo.Visibility = v1pb.Visibility_PUBLIC
	case "protected":
		memo.Visibility = v1pb.Visibility_PROTECTED
	case "private":
		memo.Visibility = v1pb.Visibility_PRIVATE
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

	_, e := s.client.MemoService.UpdateMemo(ctx, &v1pb.UpdateMemoRequest{
		Memo: memo,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"visibility", "pinned"},
		},
	})
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
		Text:        fmt.Sprintf("Memo updated as %s with [%s](%s/m/%s) %s", v1pb.Visibility_name[int32(memo.Visibility)], memo.Name, s.config.ServerAddr, memoUID, pinnedMarker),
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
	searchString := strings.TrimPrefix(m.Message.Text, "/search ")
	accessToken, _ := s.store.GetUserAccessToken(userID)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("Authorization", fmt.Sprintf("Bearer %s", accessToken)))
	currentSessionResponse, err := s.client.AuthService.GetCurrentSession(ctx, &v1pb.GetCurrentSessionRequest{})
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
			Text:   "Invalid access token",
		})
		return
	}
	user := currentSessionResponse.User
	results, err := s.client.MemoService.ListMemos(ctx, &v1pb.ListMemosRequest{
		PageSize: 10,
		Filter:   fmt.Sprintf("content.contains('%s')", searchString),
	})
	if err != nil {
		slog.Error("failed to search memos", slog.Any("err", err))
		return
	}

	memos := results.GetMemos()

	if len(memos) == 0 {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
			Text:   "No memos found for the specified search criteria.",
		})
	} else {
		for _, memo := range results.GetMemos() {
			tgMessage := memo.Name + "\n" + memo.Content
			b.SendMessage(ctx, &bot.SendMessageParams{
				ChatID: m.Message.Chat.ID,
				Text:   tgMessage,
			})
		}
	}
}

func (s *Service) processFileMessage(ctx context.Context, b *bot.Bot, m *models.Update, fileID string, memo *v1pb.Memo) {
	file, err := b.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		s.sendError(b, m.Message.Chat.ID, errors.Wrap(err, "failed to get file"))
		return
	}

	_, err = s.saveAttachmentFromFile(ctx, file, memo)
	if err != nil {
		s.sendError(b, m.Message.Chat.ID, errors.Wrap(err, "failed to save attachment"))
		return
	}
}

func (s *Service) saveAttachmentFromFile(ctx context.Context, file *models.File, memo *v1pb.Memo) (*v1pb.Attachment, error) {
	fileLink := s.bot.FileDownloadLink(file)
	response, err := http.Get(fileLink)
	if err != nil {
		return nil, errors.Wrap(err, "failed to download file")
	}
	defer response.Body.Close()

	bytes, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read file")
	}
	contentType, err := getContentType(fileLink)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get content type")
	}

	attachment, err := s.client.AttachmentService.CreateAttachment(ctx, &v1pb.CreateAttachmentRequest{
		Attachment: &v1pb.Attachment{
			Filename: filepath.Base(file.FilePath),
			Type:     contentType,
			Size:     file.FileSize,
			Content:  bytes,
			Memo:     &memo.Name,
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create attachment")
	}

	return attachment, nil
}

func (s *Service) sendError(b *bot.Bot, chatID int64, err error) {
	slog.Error("error", slog.Any("err", err))
	b.SendMessage(context.Background(), &bot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("Error: %s", err.Error()),
	})
}

func formatContent(content string, contentEntities []models.MessageEntity) string {
	contentRunes := utf16.Encode([]rune(content))

	var sb strings.Builder
	var prevEntity = models.MessageEntity{}
	var entityContent string
	re := regexp.MustCompile(`^(\s*)(.*)(\s*)$`)

	for _, entity := range contentEntities {
		switch entity.Type {
		case models.MessageEntityTypeURL:
		case models.MessageEntityTypeTextLink:
		case models.MessageEntityTypeBold:
		case models.MessageEntityTypeItalic:
		default:
			continue
		}

		if entity.Offset >= prevEntity.Offset+prevEntity.Length {
			sb.WriteString(entityContent)
			sb.WriteString(string(utf16.Decode(contentRunes[prevEntity.Offset+prevEntity.Length : entity.Offset])))
			entityContent = string(utf16.Decode(contentRunes[entity.Offset : entity.Offset+entity.Length]))
			prevEntity = entity
			if strings.TrimSpace(entityContent) == "" {
				continue
			}
		}

		matches := re.FindStringSubmatch(entityContent)
		switch entity.Type {
		case models.MessageEntityTypeURL:
			entityContent = fmt.Sprintf("%s[%s](%s)%s", matches[1], matches[2], matches[2], matches[3])
		case models.MessageEntityTypeTextLink:
			entityContent = fmt.Sprintf("%s[%s](%s)%s", matches[1], matches[2], entity.URL, matches[3])
		case models.MessageEntityTypeBold:
			entityContent = fmt.Sprintf("%s**%s**%s", matches[1], matches[2], matches[3])
		case models.MessageEntityTypeItalic:
			entityContent = fmt.Sprintf("%s*%s*%s", matches[1], matches[2], matches[3])
		}
	}
	sb.WriteString(entityContent)
	sb.WriteString(string(utf16.Decode(contentRunes[prevEntity.Offset+prevEntity.Length:])))
	return sb.String()
}
