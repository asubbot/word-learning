package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
				{"message": map[string]any{"content": `{"front":"banished","back":"изгнанный","pronunciation":"/banished/","example":"desc","conjugation":""}`}},
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
	if card.Front != "banished" || card.Back != "изгнанный" {
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

func TestGenerateCard_InvalidPayloadEmptyFront(t *testing.T) {
	t.Parallel()
	promptsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptsDir, "prompt_en-ru.txt"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"front":"   ","back":"ok","pronunciation":"","example":"","conjugation":""}`}},
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
	}); err == nil || !strings.Contains(err.Error(), "front is empty") {
		t.Fatalf("expected empty front validation error, got %v", err)
	}
}

func TestGenerateCard_InvalidPayloadEmptyBack(t *testing.T) {
	t.Parallel()
	promptsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptsDir, "prompt_en-ru.txt"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"front":"banished","back":"   ","pronunciation":"","example":"","conjugation":""}`}},
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
	}); err == nil || !strings.Contains(err.Error(), "back is empty") {
		t.Fatalf("expected empty back validation error, got %v", err)
	}
}

func TestGenerateCard_InvalidPayloadUnknownField(t *testing.T) {
	t.Parallel()
	promptsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptsDir, "prompt_en-ru.txt"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": `{"front":"banished","back":"ok","pronunciation":"","example":"","conjugation":"","extra":"x"}`}},
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
		t.Fatal("expected unknown field validation error")
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
				{"message": map[string]any{"content": `{"front":"banished","back":"ok","pronunciation":"","example":"","conjugation":""}`}},
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
				{"message": map[string]any{"content": `{"front":"banished","back":"ok","pronunciation":"","example":"","conjugation":""}`}},
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
				{"message": map[string]any{"content": `{"front":"banished","back":"ok","pronunciation":"","example":"","conjugation":""}`}},
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
				{"message": map[string]any{"content": `{"front":"banished","back":"ok","pronunciation":"","example":"","conjugation":""}`}},
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

func TestIsRetryable_ProviderErrorRetryableTrue(t *testing.T) {
	err := &ProviderError{Op: "test", Retryable: true, Err: errors.New("inner")}
	if !isRetryable(err) {
		t.Error("expected true for ProviderError with Retryable true")
	}
}

func TestIsRetryable_ProviderErrorRetryableFalse(t *testing.T) {
	err := &ProviderError{Op: "test", Retryable: false, Err: errors.New("inner")}
	if isRetryable(err) {
		t.Error("expected false for ProviderError with Retryable false")
	}
}

func TestIsRetryable_MessageContainsRetryable(t *testing.T) {
	err := errors.New("something retryable happened")
	if !isRetryable(err) {
		t.Error("expected true when error message contains \"retryable\"")
	}
}

func TestIsRetryable_PlainErrorNotRetryable(t *testing.T) {
	err := errors.New("permanent failure")
	if isRetryable(err) {
		t.Error("expected false for plain error without \"retryable\"")
	}
}

func TestIsRetryable_Nil(t *testing.T) {
	if isRetryable(nil) {
		t.Error("expected false for nil error")
	}
}

// TestGenerateCard_RetryableExhausted verifies that retryable errors cause retries
// up to MaxRetries and then return the last error.
func TestGenerateCard_RetryableExhausted(t *testing.T) {
	t.Parallel()
	promptsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptsDir, "prompt_en-ru.txt"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limit"}`))
	}))
	defer server.Close()

	gen := NewOpenAIGenerator(Config{
		APIKey:     "k",
		BaseURL:    server.URL,
		Model:      "m",
		TimeoutSec: 2,
		MaxRetries: 2,
		PromptsDir: promptsDir,
	})
	_, err := gen.GenerateCard(context.Background(), GenerateCardRequest{
		LanguageFrom: "EN",
		LanguageTo:   "RU",
		Front:        "word",
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	// MaxRetries=2 -> 3 attempts (0, 1, 2)
	if attempts != 3 {
		t.Fatalf("expected 3 attempts (retries exhausted), got %d", attempts)
	}
}

// TestGenerateCard_NonRetryableNoRetry verifies that a non-retryable error
// returns immediately without retrying (single attempt).
func TestGenerateCard_NonRetryableNoRetry(t *testing.T) {
	t.Parallel()
	promptsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(promptsDir, "prompt_en-ru.txt"), []byte("prompt"), 0o644); err != nil {
		t.Fatalf("write prompt file: %v", err)
	}

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer server.Close()

	gen := NewOpenAIGenerator(Config{
		APIKey:     "k",
		BaseURL:    server.URL,
		Model:      "m",
		TimeoutSec: 2,
		MaxRetries: 2,
		PromptsDir: promptsDir,
	})
	_, err := gen.GenerateCard(context.Background(), GenerateCardRequest{
		LanguageFrom: "EN",
		LanguageTo:   "RU",
		Front:        "word",
	})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt (no retry for non-retryable), got %d", attempts)
	}
}
