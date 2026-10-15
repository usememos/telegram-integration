package memogram

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/caarlos0/env"
	"github.com/joho/godotenv"
)

type Config struct {
	ServerAddr       string `env:"SERVER_ADDR,required"`
	BotToken         string `env:"BOT_TOKEN,required"`
	BotProxyAddr     string `env:"BOT_PROXY_ADDR"`
	Data             string `env:"DATA"`
	AllowedUsernames string `env:"ALLOWED_USERNAMES"`
}

func getConfigFromEnv() (*Config, error) {
	envFileName := ".env"
	if _, err := os.Stat(envFileName); err == nil {
		if err := godotenv.Load(envFileName); err != nil {
			return nil, fmt.Errorf("load %s: %w", envFileName, err)
		}
	}

	config := Config{}
	if err := env.Parse(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	var err error
	config.ServerAddr, err = normalizeServerAddr(config.ServerAddr)
	if err != nil {
		return nil, err
	}
	if config.Data == "" {
		// Default to `data.txt` if not specified.
		config.Data = "data.txt"
	}

	fileInfo, err := os.Stat(config.Data)
	if err != nil {
		if os.IsNotExist(err) {
			file, err := os.OpenFile(config.Data, os.O_CREATE|os.O_WRONLY, 0600)
			if err != nil {
				return nil, fmt.Errorf("failed to create config file %s: %w", config.Data, err)
			}
			file.Close()

			// Get file info after creating the file
			fileInfo, err = os.Stat(config.Data)
			if err != nil {
				return nil, fmt.Errorf("failed to get file info after creating %s: %w", config.Data, err)
			}
		} else {
			return nil, fmt.Errorf("failed to access config file %s: %w", config.Data, err)
		}
	}

	if fileInfo.IsDir() {
		return nil, fmt.Errorf("config file cannot be a directory: %s", config.Data)
	}

	config.Data, err = filepath.Abs(config.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for config file %s: %w", config.Data, err)
	}

	return &config, nil
}

func normalizeServerAddr(addr string) (string, error) {
	addr = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(addr), "dns:"))
	if addr == "" {
		return "", fmt.Errorf("SERVER_ADDR must not be empty")
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("SERVER_ADDR must be an HTTP or HTTPS base URL without credentials, query or fragment")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
