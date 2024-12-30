package memogram

import (
	"os"
	"path"

	"github.com/caarlos0/env"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
)

type Config struct {
	ServerAddr    string `env:"SERVER_ADDR,required"`
	BotToken      string `env:"BOT_TOKEN,required"`
	BotProxyAddr  string `env:"BOT_PROXY_ADDR"`
	Data          string `env:"DATA"`
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
	config.Data = path.Join(".", config.Data)
	return &config, nil
}
