package memogram

import (
	"context"
	"fmt"
	"log/slog"
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

	return &Service{
		config,
		client,
	}, nil
}

func (s *Service) Start(ctx context.Context) {
	config, err := getConfigFromEnv()
	if err != nil {
		slog.Error("failed to get config from env", slog.Any("err", err))
		return
	}

	opts := []bot.Option{
		bot.WithDefaultHandler(s.handler),
	}

	b, err := bot.New(config.BotToken, opts...)
	if err != nil {
		slog.Error("failed to create bot", slog.Any("err", err))
		return
	}

	slog.Info("memogram started")
	b.Start(ctx)
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
	text := message.Text
	if text == "" {
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
			Text:   "Please input memo content",
		})
		return
	}

	accessToken, _ := userAccessTokenCache.Load(userID)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("Authorization", fmt.Sprintf("Bearer %s", accessToken.(string))))
	memo, err := s.client.MemoService.CreateMemo(ctx, &v1pb.CreateMemoRequest{
		Content: text,
	})
	if err != nil {
		slog.Error("failed to create memo", slog.Any("err", err))
		b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: m.Message.Chat.ID,
			Text:   "Failed to create memo",
		})
		return
	}

	b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: m.Message.Chat.ID,
		Text:   fmt.Sprintf("Memo created with %s", memo.Name),
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
