package memogram

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/pkg/errors"
	v1pb "github.com/usememos/memos/proto/gen/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// userAccessTokenCache is a cache for user access token.
// Key is the user id from telegram.
// Value is the access token from memos.
// TODO: save it to a persistent storage.
var userAccessTokenCache sync.Map // map[int64]string

type Service struct {
	config *Config
	client *MemosClient
	bot    *bot.Bot
}

func NewService() (*Service, error) {
	config, err := getConfigFromEnv()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get config from env")
	}

	conn, err := grpc.Dial(config.ServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("failed to connect to server", slog.Any("err", err))
		return nil, errors.Wrap(err, "failed to connect to server")
	}
	client := NewMemosClient(conn)

	s := &Service{
		config: config,
		client: client,
	}

	opts := []bot.Option{
		bot.WithDefaultHandler(s.handler),
	}

	b, err := bot.New(config.BotToken, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create bot")
	}
	s.bot = b

	if config.AccessToken != "" {
		parts := strings.Split(config.AccessToken, ":")
		if len(parts) == 2 {
			userIDStr := parts[0]
			accessToken := parts[1]
			userID, err := strconv.ParseInt(userIDStr, 10, 64)
			if err != nil {
				slog.Error("failed to parse userID to int64", slog.String("userID", userIDStr))
				return nil, err
			}
			userAccessTokenCache.Store(userID, accessToken)
			slog.Info("load accessToken", slog.Any("userID", userID))
		}
	}

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
	s.bot.Start(ctx)
}

func (s *Service) handler(ctx context.Context, b *bot.Bot, m *models.Update) {
	if strings.HasPrefix(m.Message.Text, "/start ") {
		s.startHandler(ctx, b, m)
		return
	}

	userID := m.Message.From.ID
	if _, ok := userAccessTokenCache.Load(userID); !ok {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
			Text:   "Please start the bot with /start <access_token>",
		})
		return
	}

	message := m.Message
	// TODO: handle message.Entities to get markdown text.
	content := message.Text
	if message.Caption != "" {
		content = message.Caption
	}

	// Add "forwarded from: originName" if message was forwarded
	if m.Message.ForwardOrigin != nil {
		var originName, originUsername string
		// Determine the type of origin
		switch origin := m.Message.ForwardOrigin; {
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

	hasResource := message.Document != nil || len(message.Photo) > 0 || message.Voice != nil || message.Video != nil
	if content == "" && !hasResource {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
			Text:   "Please input memo content",
		})
		return
	}

	accessToken, _ := userAccessTokenCache.Load(userID)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("Authorization", fmt.Sprintf("Bearer %s", accessToken.(string))))
	memo, err := s.client.MemoService.CreateMemo(ctx, &v1pb.CreateMemoRequest{
		Content: content,
	})
	if err != nil {
		slog.Error("failed to create memo", slog.Any("err", err))
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
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

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:              m.Message.Chat.ID,
		Text:                fmt.Sprintf("Content saved with [%s](%s/m/%s)", memo.Name, s.config.ServerAddr, memo.Uid),
		ParseMode:           models.ParseModeMarkdown,
		DisableNotification: true,
		ReplyParameters: &models.ReplyParameters{
			MessageID: message.ID,
		},
	})
}

func (s *Service) startHandler(ctx context.Context, b *bot.Bot, m *models.Update) {
	userID := m.Message.From.ID
	accessToken := strings.TrimPrefix(m.Message.Text, "/start ")

	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("Authorization", fmt.Sprintf("Bearer %s", accessToken)))
	user, err := s.client.AuthService.GetAuthStatus(ctx, &v1pb.GetAuthStatusRequest{})
	if err != nil {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
			Text:   "Invalid access token",
		})
		return
	}

	userAccessTokenCache.Store(userID, accessToken)
	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: m.Message.Chat.ID,
		Text:   fmt.Sprintf("Hello %s!", user.Nickname),
	})
}

func (s *Service) saveResourceFromFile(ctx context.Context, file *models.File, memo *v1pb.Memo) (*v1pb.Resource, error) {
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

	resource, err := s.client.ResourceService.CreateResource(ctx, &v1pb.CreateResourceRequest{
		Resource: &v1pb.Resource{
			Filename: filepath.Base(file.FilePath),
			Type:     contentType,
			Size:     file.FileSize,
			Content:  bytes,
			Memo:     &memo.Name,
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create resource")
	}

	return resource, nil
}

func (s *Service) processFileMessage(ctx context.Context, b *bot.Bot, m *models.Update, fileID string, memo *v1pb.Memo) {
	file, err := b.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		s.sendError(b, m.Message.Chat.ID, errors.Wrap(err, "failed to get file"))
		return
	}

	_, err = s.saveResourceFromFile(ctx, file, memo)
	if err != nil {
		s.sendError(b, m.Message.Chat.ID, errors.Wrap(err, "failed to save resource"))
		return
	}
}

func (s *Service) sendError(b *bot.Bot, chatID int64, err error) {
	slog.Error("error", slog.Any("err", err))
	b.SendMessage(context.Background(), &bot.SendMessageParams{
		ChatID: chatID,
		Text:   fmt.Sprintf("Error: %s", err.Error()),
	})
}
