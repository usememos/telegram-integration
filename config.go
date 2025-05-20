package memogram

import (
	"os"
	"path/filepath"

	"github.com/caarlos0/env"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
)

type Config struct {
	ServerAddr    string `env:"SERVER_ADDR,required"`
	BotToken      string `env:"BOT_TOKEN,required"`
	BotProxyAddr  string `env:"BOT_PROXY_ADDR"`
	Data          string `env:"DATA"`
	AllowedUsernames string `env:"ALLOWED_USERNAMES"`
}

func getConfigFromEnv() (*Config, error) {
	envFileName := ".env"
	if _, err := os.Stat(envFileName); err == nil {
		err := godotenv.Load(envFileName)
		if err != nil {
			panic(err.Error())
		}
	}

	config := Config{}
	if err := env.Parse(&config); err != nil {
		return nil, errors.Wrap(err, "invalid configuration")
	}
	if config.Data == "" {
		// Default to `data.txt` if not specified.
		config.Data = "data.txt"
	}

	fileInfo, err := os.Stat(config.Data)
	if err != nil {
		if os.IsNotExist(err) {
			// Create the file with default permissions
			file, err := os.OpenFile(config.Data, os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to create config file: %s", config.Data)
			}
			file.Close()
		} else {
			return nil, errors.Wrapf(err, "failed to access config file: %s", config.Data)
		}
	}

	if fileInfo.IsDir() {
		return nil, errors.Errorf("config file cannot be a directory: %s", config.Data)
	}
	
	config.Data, err = filepath.Abs(config.Data)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get absolute path for config file: %s", config.Data)
	}

	
	return &config, nil
}
