package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"word-learning-cli/internal/domain"
	"word-learning-cli/internal/storage/sqlite"
)

var languageCodePattern = regexp.MustCompile(`^[A-Za-z]{2,8}$`)

type Service struct {
	store *sqlite.Store
}

func NewService(store *sqlite.Store) *Service {
	return &Service{store: store}
}

func (s *Service) CreateDeck(ctx context.Context, name, languageFrom, languageTo string) (domain.Deck, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Deck{}, fmt.Errorf("deck name must not be empty")
	}

	normalizedFrom, err := normalizeLanguageCode(languageFrom)
	if err != nil {
		return domain.Deck{}, fmt.Errorf("invalid --from language: %w", err)
	}
	normalizedTo, err := normalizeLanguageCode(languageTo)
	if err != nil {
		return domain.Deck{}, fmt.Errorf("invalid --to language: %w", err)
	}

	if normalizedFrom == normalizedTo {
		return domain.Deck{}, fmt.Errorf("language pair must be different")
	}

	return s.store.CreateDeck(ctx, name, normalizedFrom, normalizedTo)
}

func (s *Service) ListDecks(ctx context.Context) ([]domain.Deck, error) {
	return s.store.ListDecks(ctx)
}

func normalizeLanguageCode(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !languageCodePattern.MatchString(trimmed) {
		return "", fmt.Errorf("expected 2-8 latin letters")
	}
	return strings.ToUpper(trimmed), nil
}
