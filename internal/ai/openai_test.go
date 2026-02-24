package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "k")
	promptsDir := t.TempDir()
	t.Setenv("OPENAI_PROMPTS_DIR", promptsDir)
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
	if cfg.PromptsDir != promptsDir {
		t.Fatalf("unexpected prompts dir: %q", cfg.PromptsDir)
	}
}

func TestLoadConfigFromEnv_InvalidTimeout(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "k")
	t.Setenv("OPENAI_PROMPTS_DIR", t.TempDir())
	t.Setenv("OPENAI_TIMEOUT_SEC", "bad")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected invalid timeout error")
	}
}

func TestLoadConfigFromEnv_PromptsDirMissing(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "k")
	t.Setenv("OPENAI_PROMPTS_DIR", filepath.Join(t.TempDir(), "missing"))
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected prompts dir error")
	}
}

func TestLoadConfigFromEnv_PromptsDirNotDirectory(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "k")
	file := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	t.Setenv("OPENAI_PROMPTS_DIR", file)
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Fatal("expected prompts dir must be a directory error")
	}
}

func TestGenerateCard_Success(t *testing.T) {
	t.Parallel()
	promptsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptsDir, "prompt_en-ru.txt"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"back":"изгнанный","pronunciation":"/banished/","example":"desc","conjugation":""}`}},
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
		PromptsDir: promptsDir,
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
	promptsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptsDir, "prompt_en-ru.txt"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

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
		PromptsDir: promptsDir,
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
	promptsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptsDir, "prompt_en-ru.txt"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

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
				{"message": map[string]any{"content": `{"back":"ok","pronunciation":"","example":"","conjugation":""}`}},
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
		PromptsDir: promptsDir,
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

func TestGenerateCard_MissingPromptFile(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"back":"ok","pronunciation":"","example":"","conjugation":""}`}},
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
		PromptsDir: t.TempDir(),
	})
	_, err := gen.GenerateCard(context.Background(), GenerateCardRequest{
		LanguageFrom: "EN",
		LanguageTo:   "RU",
		Front:        "banished",
	})
	if err == nil {
		t.Fatal("expected missing prompt file error")
	}
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
}

func TestGenerateCard_EmptyPromptFile(t *testing.T) {
	t.Parallel()
	promptsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptsDir, "prompt_en-ru.txt"), []byte("   "), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"back":"ok","pronunciation":"","example":"","conjugation":""}`}},
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
		PromptsDir: promptsDir,
	})
	if _, err := gen.GenerateCard(context.Background(), GenerateCardRequest{
		LanguageFrom: "EN",
		LanguageTo:   "RU",
		Front:        "banished",
	}); err == nil {
		t.Fatal("expected empty prompt file error")
	}
}

func TestGenerateCard_NormalizesLanguagePairInPromptFileName(t *testing.T) {
	t.Parallel()
	promptsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptsDir, "prompt_en-ru.txt"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"back":"ok","pronunciation":"","example":"","conjugation":""}`}},
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
		PromptsDir: promptsDir,
	})
	card, err := gen.GenerateCard(context.Background(), GenerateCardRequest{
		LanguageFrom: " En ",
		LanguageTo:   " rU ",
		Front:        "banished",
	})
	if err != nil {
		t.Fatalf("GenerateCard: %v", err)
	}
	if card.Back != "ok" {
		t.Fatalf("unexpected card: %#v", card)
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
		PromptsDir: t.TempDir(),
	})
	if gen.client.Timeout != 7*time.Second {
		t.Fatalf("unexpected timeout: %v", gen.client.Timeout)
	}
}
