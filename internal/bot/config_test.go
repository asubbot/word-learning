package bot

import (
	"strings"
	"testing"
)

func TestLoadConfigFromEnv_SuccessAndDedupAllowedUsers(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "token")
	t.Setenv("WORDLEARN_DB_PATH", "/tmp/test.db")
	t.Setenv("TELEGRAM_POLLING_TIMEOUT", "15")
	t.Setenv("BOT_ALLOWED_USER_IDS", "42, 77,42,  99 ")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if cfg.TelegramBotToken != "token" {
		t.Fatalf("unexpected token: %q", cfg.TelegramBotToken)
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Fatalf("unexpected db path: %q", cfg.DBPath)
	}
	if cfg.PollingTimeout != 15 {
		t.Fatalf("unexpected polling timeout: %d", cfg.PollingTimeout)
	}
	if len(cfg.AllowedUserIDs) != 3 || cfg.AllowedUserIDs[0] != 42 || cfg.AllowedUserIDs[1] != 77 || cfg.AllowedUserIDs[2] != 99 {
		t.Fatalf("unexpected allowed ids: %#v", cfg.AllowedUserIDs)
	}
}

func TestLoadConfigFromEnv_RequiresVarsAndValidTimeout(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("WORDLEARN_DB_PATH", "")
	t.Setenv("TELEGRAM_POLLING_TIMEOUT", "")
	t.Setenv("BOT_ALLOWED_USER_IDS", "")

	if _, err := LoadConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "TELEGRAM_BOT_TOKEN is required") {
		t.Fatalf("expected missing token error, got %v", err)
	}

	t.Setenv("TELEGRAM_BOT_TOKEN", "token")
	if _, err := LoadConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "WORDLEARN_DB_PATH") {
		t.Fatalf("expected missing db path error, got %v", err)
	}

	t.Setenv("WORDLEARN_DB_PATH", "/tmp/test.db")
	t.Setenv("TELEGRAM_POLLING_TIMEOUT", "0")
	if _, err := LoadConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "TELEGRAM_POLLING_TIMEOUT must be a positive integer") {
		t.Fatalf("expected invalid timeout error, got %v", err)
	}
}

func TestParseAllowedUserIDsEnv_InvalidValue(t *testing.T) {
	t.Setenv("BOT_ALLOWED_USER_IDS", "42,abc")
	if _, err := parseAllowedUserIDsEnv("BOT_ALLOWED_USER_IDS"); err == nil {
		t.Fatal("expected invalid allowed user ids error")
	}
}
