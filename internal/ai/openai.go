package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	APIKey     string
	BaseURL    string
	Model      string
	TimeoutSec int
	MaxRetries int
	PromptsDir string
}

type OpenAIGenerator struct {
	cfg    Config
	client *http.Client
}

func LoadConfigFromEnv() (Config, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return Config{}, fmt.Errorf("OPENAI_API_KEY is required")
	}
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if model == "" {
		model = "gpt-4o-mini"
	}
	timeoutSec := 30
	if raw := strings.TrimSpace(os.Getenv("OPENAI_TIMEOUT_SEC")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("OPENAI_TIMEOUT_SEC must be a positive integer")
		}
		timeoutSec = parsed
	}
	maxRetries := 2
	if raw := strings.TrimSpace(os.Getenv("OPENAI_MAX_RETRIES")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return Config{}, fmt.Errorf("OPENAI_MAX_RETRIES must be a non-negative integer")
		}
		maxRetries = parsed
	}
	promptsDir := strings.TrimSpace(os.Getenv("OPENAI_PROMPTS_DIR"))
	if promptsDir == "" {
		promptsDir = "./prompts"
	}
	info, err := os.Stat(promptsDir)
	if err != nil {
		return Config{}, fmt.Errorf("OPENAI_PROMPTS_DIR invalid: %w", err)
	}
	if !info.IsDir() {
		return Config{}, fmt.Errorf("OPENAI_PROMPTS_DIR must point to a directory")
	}
	return Config{
		APIKey:     apiKey,
		BaseURL:    strings.TrimSuffix(baseURL, "/"),
		Model:      model,
		TimeoutSec: timeoutSec,
		MaxRetries: maxRetries,
		PromptsDir: promptsDir,
	}, nil
}

func NewOpenAIGenerator(cfg Config) *OpenAIGenerator {
	return &OpenAIGenerator{
		cfg: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
		},
	}
}

func NewGeneratorFromEnv() (Generator, error) {
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return NewOpenAIGenerator(cfg), nil
}

type chatCompletionsRequest struct {
	Model       string                   `json:"model"`
	Messages    []chatCompletionsMessage `json:"messages"`
	Temperature float64                  `json:"temperature"`
}

type chatCompletionsMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionsResponse struct {
	Choices []struct {
		Message chatCompletionsMessage `json:"message"`
	} `json:"choices"`
}

type generatedPayload struct {
	Back          string `json:"back"`
	Pronunciation string `json:"pronunciation"`
	Example       string `json:"example"`
	Conjugation   string `json:"conjugation"`
}

func (g *OpenAIGenerator) GenerateCard(ctx context.Context, req GenerateCardRequest) (GeneratedCard, error) {
	var lastErr error
	for attempt := 0; attempt <= g.cfg.MaxRetries; attempt++ {
		card, err := g.generateOnce(ctx, req)
		if err == nil {
			return card, nil
		}
		lastErr = err
		var providerErr *ProviderError
		if attempt == g.cfg.MaxRetries || !strings.Contains(err.Error(), "retryable") && (!asProviderError(err, &providerErr) || !providerErr.Retryable) {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}
	return GeneratedCard{}, lastErr
}

func asProviderError(err error, target **ProviderError) bool {
	if err == nil {
		return false
	}
	providerErr, ok := err.(*ProviderError)
	if !ok {
		return false
	}
	*target = providerErr
	return true
}

func resolveSystemPrompt(promptsDir, languageFrom, languageTo string) (string, error) {
	from := strings.ToLower(strings.TrimSpace(languageFrom))
	to := strings.ToLower(strings.TrimSpace(languageTo))
	if from == "" || to == "" {
		return "", fmt.Errorf("language pair is required for prompt resolution")
	}
	promptPath := filepath.Join(promptsDir, fmt.Sprintf("prompt_%s-%s.txt", from, to))
	b, err := os.ReadFile(promptPath)
	if err != nil {
		return "", fmt.Errorf("prompt file for pair %s-%s not found/readable at %s: %w", from, to, promptPath, err)
	}
	prompt := strings.TrimSpace(string(b))
	if prompt == "" {
		return "", fmt.Errorf("prompt file for pair %s-%s is empty: %s", from, to, promptPath)
	}
	return prompt, nil
}

func (g *OpenAIGenerator) generateOnce(ctx context.Context, req GenerateCardRequest) (GeneratedCard, error) {
	systemPrompt, err := resolveSystemPrompt(g.cfg.PromptsDir, req.LanguageFrom, req.LanguageTo)
	if err != nil {
		return GeneratedCard{}, &ProviderError{Op: "resolve system prompt", Retryable: false, Err: err}
	}
	payload := chatCompletionsRequest{
		Model: g.cfg.Model,
		Messages: []chatCompletionsMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: fmt.Sprintf("language_from=%s\nlanguage_to=%s\nfront=%s", req.LanguageFrom, req.LanguageTo, req.Front)},
		},
		Temperature: 0.2,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return GeneratedCard{}, &ProviderError{Op: "marshal request", Retryable: false, Err: err}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return GeneratedCard{}, &ProviderError{Op: "build request", Retryable: false, Err: err}
	}
	httpReq.Header.Set("Authorization", "Bearer "+g.cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return GeneratedCard{}, &ProviderError{Op: "request", Retryable: true, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return GeneratedCard{}, &ProviderError{Op: "read response", Retryable: true, Err: err}
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return GeneratedCard{}, &ProviderError{Op: "api retryable", Retryable: true, Err: fmt.Errorf("retryable status %d", resp.StatusCode)}
	}
	if resp.StatusCode >= 400 {
		return GeneratedCard{}, &ProviderError{Op: "api response", Retryable: false, Err: fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))}
	}

	var completion chatCompletionsResponse
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return GeneratedCard{}, &ProviderError{Op: "decode completion", Retryable: false, Err: err}
	}
	if len(completion.Choices) == 0 {
		return GeneratedCard{}, &ProviderError{Op: "decode completion", Retryable: false, Err: fmt.Errorf("empty choices")}
	}
	content := strings.TrimSpace(completion.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var generated generatedPayload
	if err := json.Unmarshal([]byte(content), &generated); err != nil {
		return GeneratedCard{}, &ProviderError{Op: "decode generated card", Retryable: false, Err: err}
	}
	return GeneratedCard(generated), nil
}
