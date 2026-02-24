package bot

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	TelegramBotToken string
	DBPath           string
	PollingTimeout   int
	AllowedUserIDs   []int64
}

func LoadConfigFromEnv() (Config, error) {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if token == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	dbPath := strings.TrimSpace(os.Getenv("WORDLEARN_DB_PATH"))
	if dbPath == "" {
		return Config{}, fmt.Errorf("database path is required: set WORDLEARN_DB_PATH")
	}

	pollingTimeout := 30
	if raw := strings.TrimSpace(os.Getenv("TELEGRAM_POLLING_TIMEOUT")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("TELEGRAM_POLLING_TIMEOUT must be a positive integer")
		}
		pollingTimeout = parsed
	}

	allowedUserIDs := make([]int64, 0)
	if raw := strings.TrimSpace(os.Getenv("BOT_ALLOWED_USER_IDS")); raw != "" {
		parts := strings.Split(raw, ",")
		seen := make(map[int64]struct{}, len(parts))
		for _, p := range parts {
			value := strings.TrimSpace(p)
			if value == "" {
				continue
			}
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				return Config{}, fmt.Errorf("BOT_ALLOWED_USER_IDS must contain positive int64 ids separated by commas")
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			allowedUserIDs = append(allowedUserIDs, id)
		}
	}

	return Config{
		TelegramBotToken: token,
		DBPath:           dbPath,
		PollingTimeout:   pollingTimeout,
		AllowedUserIDs:   allowedUserIDs,
	}, nil
}
