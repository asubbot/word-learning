package ai

import "context"

type GenerateCardRequest struct {
	LanguageFrom string
	LanguageTo   string
	Front        string
}

type GeneratedCard struct {
	Back          string
	Pronunciation string
	Example       string
	Conjugation   string
}

type Generator interface {
	GenerateCard(ctx context.Context, req GenerateCardRequest) (GeneratedCard, error)
}

type ProviderError struct {
	Op        string
	Retryable bool
	Err       error
}

func (e *ProviderError) Error() string {
	if e == nil {
		return ""
	}
	return e.Op + ": " + e.Err.Error()
}

func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
