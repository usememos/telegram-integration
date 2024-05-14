package memogram

import (
	"github.com/caarlos0/env"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
)

type Config struct {
	ServerAddr string `env:"SERVER_ADDR,required"`
	BotToken   string `env:"BOT_TOKEN,required"`
}

func getConfigFromEnv() (*Config, error) {
	err := godotenv.Load(".env")
	if err != nil {
		panic(err.Error())
	}

	config := Config{}
	if err := env.Parse(&config); err != nil {
		return nil, errors.Wrap(err, "invalid configuration")
	}
	return &config, nil
}
