// Package config reads Memogram's settings from the environment.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/joho/godotenv"

	"github.com/usememos/memogram/internal/memos"
)

// DefaultDataPath is used when DATA is unset. Docker images set DATA to a path
// inside their persistent volume.
const DefaultDataPath = "data.txt"

type Config struct {
	// ServerAddr is the normalized Memos base URL (SERVER_ADDR).
	ServerAddr string
	// BotToken is the Telegram bot token (BOT_TOKEN).
	BotToken string
	// BotProxyAddr optionally replaces the Telegram Bot API server (BOT_PROXY_ADDR).
	BotProxyAddr string
	// DataPath is the file that stores user bindings (DATA).
	DataPath string
	// AllowedUsernames lists lowercase Telegram usernames allowed to use the
	// bot (ALLOWED_USERNAMES, comma-separated). Empty allows everyone.
	AllowedUsernames []string
}

// Load reads the configuration from the environment, after loading a .env
// file from the working directory if one exists. Variables already set in the
// environment take precedence over .env.
func Load() (*Config, error) {
	const envFile = ".env"
	if err := godotenv.Load(envFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("load %s: %w", envFile, err)
	}
	return FromEnv(os.Getenv)
}

// FromEnv builds and validates a configuration from the given lookup function.
func FromEnv(getenv func(string) string) (*Config, error) {
	cfg := &Config{
		BotToken:         strings.TrimSpace(getenv("BOT_TOKEN")),
		BotProxyAddr:     strings.TrimSpace(getenv("BOT_PROXY_ADDR")),
		DataPath:         strings.TrimSpace(getenv("DATA")),
		AllowedUsernames: parseUsernames(getenv("ALLOWED_USERNAMES")),
	}
	serverAddr := getenv("SERVER_ADDR")
	if strings.TrimSpace(serverAddr) == "" {
		return nil, errors.New("SERVER_ADDR is required")
	}
	var err error
	if cfg.ServerAddr, err = memos.NormalizeBaseURL(serverAddr); err != nil {
		return nil, fmt.Errorf("SERVER_ADDR %w", err)
	}
	if cfg.BotToken == "" {
		return nil, errors.New("BOT_TOKEN is required")
	}
	if cfg.DataPath == "" {
		cfg.DataPath = DefaultDataPath
	}
	return cfg, nil
}

func parseUsernames(raw string) []string {
	var usernames []string
	for entry := range strings.SplitSeq(raw, ",") {
		if username := strings.ToLower(strings.TrimSpace(entry)); username != "" {
			usernames = append(usernames, username)
		}
	}
	return usernames
}
