package config

import (
	"reflect"
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestFromEnv(t *testing.T) {
	cfg, err := FromEnv(env(map[string]string{
		"SERVER_ADDR":       "dns:localhost:5230/",
		"BOT_TOKEN":         " 123:token ",
		"BOT_PROXY_ADDR":    "http://telegram-proxy:8081",
		"ALLOWED_USERNAMES": " Alice, ,bob ,",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := &Config{
		ServerAddr:       "http://localhost:5230",
		BotToken:         "123:token",
		BotProxyAddr:     "http://telegram-proxy:8081",
		DataPath:         DefaultDataPath,
		AllowedUsernames: []string{"alice", "bob"},
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestFromEnvRejectsInvalidSettings(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		values     map[string]string
	}{
		{"missing server", "SERVER_ADDR is required", map[string]string{"BOT_TOKEN": "t"}},
		{"blank server", "SERVER_ADDR is required", map[string]string{"SERVER_ADDR": " ", "BOT_TOKEN": "t"}},
		{"invalid server", "SERVER_ADDR must be an HTTP", map[string]string{"SERVER_ADDR": "ftp://memos", "BOT_TOKEN": "t"}},
		{"missing token", "BOT_TOKEN is required", map[string]string{"SERVER_ADDR": "localhost:5230"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := FromEnv(env(tc.values)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestFromEnvKeepsDataPath(t *testing.T) {
	cfg, err := FromEnv(env(map[string]string{"SERVER_ADDR": "localhost", "BOT_TOKEN": "t", "DATA": "/data/data.txt"}))
	if err != nil || cfg.DataPath != "/data/data.txt" || cfg.AllowedUsernames != nil {
		t.Fatalf("got %+v, %v", cfg, err)
	}
}
