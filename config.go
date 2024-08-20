package memogram

import (
	"os"
	"github.com/caarlos0/env"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
)

type Config struct {
	ServerAddr  string `env:"SERVER_ADDR,required"`
	BotToken    string `env:"BOT_TOKEN,required"`
	MemosAddr		string `env:"MEMOS_ADDR"`
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
	return &config, nil
}
