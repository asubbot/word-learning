package bot

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	TelegramBotToken            string
	DBPath                      string
	PollingTimeout              int
	AllowedUserIDs              []int64
	ReminderIntervalMin         int
	ReminderMinOverdue          int
	ReminderMinHoursSinceReview float64
}

func LoadConfigFromEnv() (Config, error) {
	token, err := requiredEnv("TELEGRAM_BOT_TOKEN")
	if err != nil {
		return Config{}, err
	}
	dbPath, err := requiredEnv("WORDLEARN_DB_PATH")
	if err != nil {
		return Config{}, fmt.Errorf("database path is required: set WORDLEARN_DB_PATH")
	}
	pollingTimeout, err := parsePositiveIntEnvWithDefault("TELEGRAM_POLLING_TIMEOUT", 30)
	if err != nil {
		return Config{}, err
	}
	allowedUserIDs, err := parseAllowedUserIDsEnv("BOT_ALLOWED_USER_IDS")
	if err != nil {
		return Config{}, err
	}
	reminderIntervalMin, err := parsePositiveIntEnvWithDefault("REMINDER_INTERVAL_MINUTES", 60)
	if err != nil {
		return Config{}, err
	}
	reminderMinOverdue, err := parsePositiveIntEnvWithDefault("REMINDER_MIN_OVERDUE", 10)
	if err != nil {
		return Config{}, err
	}
	reminderMinHours, err := parseFloatEnvWithDefault("REMINDER_MIN_HOURS_SINCE_REVIEW", 12)
	if err != nil {
		return Config{}, err
	}

	return Config{
		TelegramBotToken:            token,
		DBPath:                      dbPath,
		PollingTimeout:              pollingTimeout,
		AllowedUserIDs:              allowedUserIDs,
		ReminderIntervalMin:         reminderIntervalMin,
		ReminderMinOverdue:          reminderMinOverdue,
		ReminderMinHoursSinceReview: reminderMinHours,
	}, nil
}

func requiredEnv(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return value, nil
}

func parsePositiveIntEnvWithDefault(name string, defaultValue int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func parseFloatEnvWithDefault(name string, defaultValue float64) (float64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s must be a non-negative number", name)
	}
	return parsed, nil
}

func parseAllowedUserIDsEnv(name string) ([]int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return []int64{}, nil
	}
	parts := strings.Split(raw, ",")
	seen := make(map[int64]struct{}, len(parts))
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		value := strings.TrimSpace(p)
		if value == "" {
			continue
		}
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("%s must contain positive int64 ids separated by commas", name)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}
