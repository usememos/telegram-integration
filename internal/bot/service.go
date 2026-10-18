// Package bot is the Telegram side of Memogram. It saves incoming messages and
// their files as memos, and handles the commands and buttons that act on them.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memos"
	"github.com/usememos/memogram/internal/memosapi"
	"github.com/usememos/memogram/internal/store"
)

// UserStore persists each Telegram user's Memos access token and settings.
type UserStore interface {
	AccessToken(userID int64) (string, bool)
	SetAccessToken(userID int64, accessToken string) error
	// DefaultSpace returns the zero Space when the user has none.
	DefaultSpace(userID int64) store.Space
	SetDefaultSpace(userID int64, space store.Space) error
}

// Options configures a Service.
type Options struct {
	// Token is the Telegram bot token.
	Token string
	// ServerURL optionally replaces the Telegram Bot API server.
	ServerURL string
	// Memos is the unauthenticated client for the Memos instance. Each request
	// derives a client that authenticates as the sending user.
	Memos *memos.Client
	// Store holds the users' access tokens and settings.
	Store UserStore
	// AllowedUsernames lists the lowercase Telegram usernames allowed to use
	// the bot. Empty allows everyone.
	AllowedUsernames []string
	// Downloads fetches files from Telegram. It defaults to a client with a
	// 60 second timeout.
	Downloads *http.Client
	// BotOptions are applied to the Telegram client after Memogram's own.
	BotOptions []bot.Option
}

type Service struct {
	bot       *bot.Bot
	memos     *memos.Client
	store     UserStore
	downloads *http.Client
	allowed   map[string]struct{}
	albums    *mediaGroups

	// spaces reports whether the Memos server supports spaces. It is set from
	// the instance version at startup and cleared if the server reports
	// otherwise.
	spaces atomic.Bool

	// linkBase is the web address used in memo links. Start sets it from the
	// instance profile before polling begins, and handlers only read it.
	linkBase string
}

const (
	commandStart  = "start"
	commandSearch = "search"
	commandSpace  = "space"
)

// New creates the Telegram client and registers Memogram's handlers. Unless
// Options.BotOptions skip it, this calls Telegram's getMe to validate the
// token.
func New(opts Options) (*Service, error) {
	if opts.Memos == nil || opts.Store == nil {
		return nil, errors.New("a Memos client and a token store are required")
	}
	s := &Service{
		memos:     opts.Memos,
		store:     opts.Store,
		downloads: opts.Downloads,
		allowed:   make(map[string]struct{}, len(opts.AllowedUsernames)),
		albums:    newMediaGroups(albumTTL),
		linkBase:  opts.Memos.BaseURL(),
	}
	if s.downloads == nil {
		s.downloads = &http.Client{Timeout: 60 * time.Second}
	}
	s.spaces.Store(true)
	for _, username := range opts.AllowedUsernames {
		s.allowed[strings.ToLower(username)] = struct{}{}
	}

	botOptions := []bot.Option{
		bot.WithMiddlewares(s.authorize),
		bot.WithDefaultHandler(s.handleMessage),
		bot.WithErrorsHandler(func(err error) {
			slog.Error("Telegram client error", slog.Any("err", redactTelegramError(err)))
		}),
	}
	if opts.ServerURL != "" {
		botOptions = append(botOptions, bot.WithServerURL(opts.ServerURL))
	}
	b, err := bot.New(opts.Token, append(botOptions, opts.BotOptions...)...)
	if err != nil {
		return nil, fmt.Errorf("create Telegram bot: %w", redactTelegramError(err))
	}
	s.bot = b
	b.RegisterHandlerMatchFunc(isCommand(commandStart), s.handleStart)
	b.RegisterHandlerMatchFunc(isCommand(commandSearch), s.handleSearch)
	b.RegisterHandlerMatchFunc(isCommand(commandSpace), s.handleSpace)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "", bot.MatchTypePrefix, s.handleCallback)
	return s, nil
}

// Start waits for Memos, then polls Telegram until ctx is canceled. Waiting
// first selects the API base path, including when Memos starts later than
// Memogram in a Compose deployment.
func (s *Service) Start(ctx context.Context) {
	slog.Info("Memogram started")
	for {
		profile, err := s.memos.GetInstanceProfile(ctx)
		if err == nil {
			s.useInstanceProfile(profile)
			slog.Info("connected to Memos", slog.String("version", profile.Version), slog.String("api", s.memos.APIPrefix()), slog.Bool("spaces", s.spaces.Load()))
			break
		}
		slog.Warn("waiting for Memos instance profile", slog.Any("err", err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}

	commands := []models.BotCommand{
		{Command: commandStart, Description: "Start the bot with access token"},
		{Command: commandSearch, Description: "Search for the memos"},
	}
	if s.spaces.Load() {
		commands = append(commands, models.BotCommand{Command: commandSpace, Description: "Choose the default space for new memos"})
	}
	if _, err := s.bot.SetMyCommands(ctx, &bot.SetMyCommandsParams{Commands: commands}); err != nil {
		slog.Error("failed to set bot commands", slog.Any("err", redactTelegramError(err)))
	}
	s.bot.Start(ctx)
}

// useInstanceProfile enables spaces when the server version has them, and
// links memos to the instance's public address when the administrator has
// configured one.
func (s *Service) useInstanceProfile(profile *memosapi.InstanceProfile) {
	s.spaces.Store(memos.SupportsSpaces(profile.Version))
	if profile.InstanceUrl == "" {
		return
	}
	if linkBase, err := memos.NormalizeBaseURL(profile.InstanceUrl); err == nil {
		s.linkBase = linkBase
	}
}

// isCommand matches "/name", alone or followed by arguments.
func isCommand(name string) bot.MatchFunc {
	command := "/" + name
	return func(update *models.Update) bool {
		if update.Message == nil {
			return false
		}
		text := update.Message.Text
		return text == command || strings.HasPrefix(text, command+" ")
	}
}

func commandArgs(text, name string) string {
	return strings.TrimSpace(strings.TrimPrefix(text, "/"+name))
}

// memoLink returns a Markdown link to the memo in the Memos web app.
func (s *Service) memoLink(memo *memosapi.Memo) (string, error) {
	uid, err := memos.MemoUID(memo.Name)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("[%s](%s/memos/%s)", memo.Name, s.linkBase, uid), nil
}
