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

var ErrActiveDeckNotSet = fmt.Errorf("active deck is not set")
var ErrDeckNameAmbiguous = fmt.Errorf("deck name is ambiguous")

type DeckUseResult struct {
	Deck       *domain.Deck
	Candidates []domain.Deck
}

func NewService(store *sqlite.Store) *Service {
	return &Service{store: store}
}

func (s *Service) CreateDeck(ctx context.Context, name, languageFrom, languageTo string) (domain.Deck, error) {
	return s.CreateDeckForUser(ctx, 0, name, languageFrom, languageTo)
}

func (s *Service) CreateDeckForUser(ctx context.Context, telegramUserID int64, name, languageFrom, languageTo string) (domain.Deck, error) {
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

	return s.store.CreateDeckForOwner(ctx, telegramUserID, name, normalizedFrom, normalizedTo)
}

func (s *Service) ListDecks(ctx context.Context) ([]domain.Deck, error) {
	return s.ListDecksForUser(ctx, 0)
}

func (s *Service) ListDecksForUser(ctx context.Context, telegramUserID int64) ([]domain.Deck, error) {
	return s.store.ListDecksForOwner(ctx, telegramUserID)
}

func (s *Service) ListDecksAll(ctx context.Context) ([]domain.Deck, error) {
	return s.store.ListDecksAll(ctx)
}

func (s *Service) GetDeckByID(ctx context.Context, deckID int64) (*domain.Deck, error) {
	return s.store.GetDeckByID(ctx, deckID)
}

func (s *Service) DeckCurrentForUser(ctx context.Context, userID int64) (*domain.Deck, error) {
	return s.store.GetActiveDeckForUser(ctx, userID)
}

func (s *Service) DeckUseForUser(ctx context.Context, userID int64, deckName string) (DeckUseResult, error) {
	trimmed := strings.TrimSpace(deckName)
	if trimmed == "" {
		return DeckUseResult{}, fmt.Errorf("deck name must not be empty")
	}
	deck, err := s.store.FindDeckByExactNameForOwner(ctx, userID, trimmed)
	if err != nil {
		return DeckUseResult{}, err
	}
	if deck != nil {
		if err := s.store.SetActiveDeckForUser(ctx, userID, deck.ID); err != nil {
			return DeckUseResult{}, err
		}
		return DeckUseResult{Deck: deck}, nil
	}
	candidates, err := s.store.FindDeckCandidatesForOwner(ctx, userID, trimmed, 10)
	if err != nil {
		return DeckUseResult{}, err
	}
	if len(candidates) > 0 {
		return DeckUseResult{Candidates: candidates}, ErrDeckNameAmbiguous
	}
	return DeckUseResult{}, fmt.Errorf("deck %q not found", trimmed)
}

func (s *Service) ResolveActiveDeckForUser(ctx context.Context, userID int64) (*domain.Deck, error) {
	deck, err := s.store.GetActiveDeckForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if deck == nil {
		return nil, ErrActiveDeckNotSet
	}
	return deck, nil
}

func (s *Service) DeckUseByIDForUser(ctx context.Context, userID, deckID int64) (*domain.Deck, error) {
	if deckID <= 0 {
		return nil, fmt.Errorf("deck id must be a positive integer")
	}
	deck, err := s.store.GetDeckForOwner(ctx, deckID, userID)
	if err != nil {
		return nil, err
	}
	if deck == nil {
		return nil, fmt.Errorf("deck %d not found", deckID)
	}
	if err := s.store.SetActiveDeckForUser(ctx, userID, deck.ID); err != nil {
		return nil, err
	}
	return deck, nil
}

func normalizeLanguageCode(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !languageCodePattern.MatchString(trimmed) {
		return "", fmt.Errorf("expected 2-8 latin letters")
	}
	return strings.ToUpper(trimmed), nil
}
