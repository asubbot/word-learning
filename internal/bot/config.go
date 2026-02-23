package bot

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	TelegramBotToken string
	DBPath           string
	PollingTimeout   int
}

func LoadConfigFromEnv() (Config, error) {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	dbPath := strings.TrimSpace(os.Getenv("WORDCLI_DB_PATH"))
	if dbPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("resolve working directory: %w", err)
		}
		dbPath = filepath.Join(cwd, "wordcli.db")
	}

	pollingTimeout := 30
	if raw := strings.TrimSpace(os.Getenv("TELEGRAM_POLLING_TIMEOUT")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("TELEGRAM_POLLING_TIMEOUT must be a positive integer")
		}
		pollingTimeout = parsed
	}

	return Config{
		TelegramBotToken: token,
		DBPath:           dbPath,
		PollingTimeout:   pollingTimeout,
	}, nil
}
