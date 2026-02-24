package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "k")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_MODEL", "")
	t.Setenv("OPENAI_TIMEOUT_SEC", "")
	t.Setenv("OPENAI_MAX_RETRIES", "")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv: %v", err)
	}
	if cfg.APIKey != "k" || cfg.Model == "" || cfg.BaseURL == "" || cfg.TimeoutSec <= 0 {
		t.Fatalf("unexpected cfg: %#v", cfg)
	}
}

func TestLoadConfigFromEnv_InvalidTimeout(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "k")
	t.Setenv("OPENAI_TIMEOUT_SEC", "bad")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected invalid timeout error")
	}
}

func TestGenerateCard_Success(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"back":"изгнанный","pronunciation":"/banished/","description":"desc"}`}},
			},
		})
	}))
	defer server.Close()

	gen := NewOpenAIGenerator(Config{
		APIKey:     "k",
		BaseURL:    server.URL,
		Model:      "m",
		TimeoutSec: 2,
		MaxRetries: 0,
	})
	card, err := gen.GenerateCard(context.Background(), GenerateCardRequest{
		LanguageFrom: "EN",
		LanguageTo:   "RU",
		Front:        "banished",
	})
	if err != nil {
		t.Fatalf("GenerateCard: %v", err)
	}
	if card.Back != "изгнанный" {
		t.Fatalf("unexpected card: %#v", card)
	}
}

func TestGenerateCard_MalformedJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `not-json`}},
			},
		})
	}))
	defer server.Close()

	gen := NewOpenAIGenerator(Config{
		APIKey:     "k",
		BaseURL:    server.URL,
		Model:      "m",
		TimeoutSec: 2,
		MaxRetries: 0,
	})
	if _, err := gen.GenerateCard(context.Background(), GenerateCardRequest{
		LanguageFrom: "EN",
		LanguageTo:   "RU",
		Front:        "banished",
	}); err == nil {
		t.Fatal("expected malformed JSON error")
	}
}

func TestGenerateCard_RetryableTimeout(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"back":"ok","pronunciation":"","description":""}`}},
			},
		})
	}))
	defer server.Close()

	gen := NewOpenAIGenerator(Config{
		APIKey:     "k",
		BaseURL:    server.URL,
		Model:      "m",
		TimeoutSec: 2,
		MaxRetries: 1,
	})
	card, err := gen.GenerateCard(context.Background(), GenerateCardRequest{
		LanguageFrom: "EN",
		LanguageTo:   "RU",
		Front:        "banished",
	})
	if err != nil {
		t.Fatalf("GenerateCard retry: %v", err)
	}
	if card.Back != "ok" {
		t.Fatalf("unexpected card: %#v", card)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestNewGeneratorFromEnvMissingKey(t *testing.T) {
	_ = os.Unsetenv("OPENAI_API_KEY")
	_, err := NewGeneratorFromEnv()
	if err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestOpenAIGeneratorTimeoutConfig(t *testing.T) {
	t.Parallel()
	gen := NewOpenAIGenerator(Config{
		APIKey:     "k",
		BaseURL:    "http://example",
		Model:      "m",
		TimeoutSec: 7,
		MaxRetries: 0,
	})
	if gen.client.Timeout != 7*time.Second {
		t.Fatalf("unexpected timeout: %v", gen.client.Timeout)
	}
}
