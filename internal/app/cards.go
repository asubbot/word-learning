package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"word-learning-cli/internal/domain"
	"word-learning-cli/internal/storage/sqlite"
)

var ErrCardNotFound = errors.New("card not found")

type DeckStats struct {
	Active  int64
	Snoozed int64
	Total   int64
}

func (s *Service) AddCard(ctx context.Context, deckID int64, front, back, pronunciation, description string) (domain.Card, error) {
	if deckID <= 0 {
		return domain.Card{}, fmt.Errorf("--deck must be a positive integer")
	}

	front = strings.TrimSpace(front)
	back = strings.TrimSpace(back)
	pronunciation = strings.TrimSpace(pronunciation)
	description = strings.TrimSpace(description)
	if front == "" {
		return domain.Card{}, fmt.Errorf("front must not be empty")
	}
	if back == "" {
		return domain.Card{}, fmt.Errorf("back must not be empty")
	}

	exists, err := s.store.DeckExists(ctx, deckID)
	if err != nil {
		return domain.Card{}, err
	}
	if !exists {
		return domain.Card{}, fmt.Errorf("deck %d does not exist", deckID)
	}

	return s.store.CreateCard(ctx, sqlite.CardCreateParams{
		DeckID:        deckID,
		Front:         front,
		Back:          back,
		Pronunciation: pronunciation,
		Description:   description,
	})
}

func (s *Service) ListCards(ctx context.Context, deckID int64, status string) ([]domain.Card, error) {
	if deckID <= 0 {
		return nil, fmt.Errorf("--deck must be a positive integer")
	}

	var statusPtr *domain.CardStatus
	if strings.TrimSpace(status) != "" {
		parsed, err := parseCardStatus(status)
		if err != nil {
			return nil, err
		}
		statusPtr = &parsed
	}

	return s.store.ListCards(ctx, deckID, statusPtr)
}

func (s *Service) RemoveCard(ctx context.Context, cardID int64) error {
	if cardID <= 0 {
		return fmt.Errorf("--id must be a positive integer")
	}

	updated, err := s.store.SetCardStatus(ctx, cardID, domain.CardStatusRemoved, nil)
	if err != nil {
		return err
	}
	if !updated {
		return ErrCardNotFound
	}
	return nil
}

func (s *Service) RestoreCard(ctx context.Context, cardID int64) error {
	if cardID <= 0 {
		return fmt.Errorf("--id must be a positive integer")
	}

	updated, err := s.store.SetCardStatus(ctx, cardID, domain.CardStatusActive, nil)
	if err != nil {
		return err
	}
	if !updated {
		return ErrCardNotFound
	}
	return nil
}

func (s *Service) RememberCard(ctx context.Context, cardID int64) error {
	if cardID <= 0 {
		return fmt.Errorf("--id must be a positive integer")
	}

	snoozedUntil := time.Now().UTC().Add(24 * time.Hour)
	updated, err := s.store.SetCardStatus(ctx, cardID, domain.CardStatusSnoozed, &snoozedUntil)
	if err != nil {
		return err
	}
	if !updated {
		return ErrCardNotFound
	}
	return nil
}

func (s *Service) DontRememberCard(ctx context.Context, cardID int64) error {
	if cardID <= 0 {
		return fmt.Errorf("--id must be a positive integer")
	}

	updated, err := s.store.SetCardStatus(ctx, cardID, domain.CardStatusActive, nil)
	if err != nil {
		return err
	}
	if !updated {
		return ErrCardNotFound
	}
	return nil
}

func (s *Service) NextCard(ctx context.Context, deckID int64) (*domain.Card, error) {
	if deckID <= 0 {
		return nil, fmt.Errorf("--deck must be a positive integer")
	}
	return s.store.NextCardForDeck(ctx, deckID, time.Now().UTC())
}

func (s *Service) NextCardWithStats(ctx context.Context, deckID int64) (*domain.Card, DeckStats, error) {
	card, err := s.NextCard(ctx, deckID)
	if err != nil {
		return nil, DeckStats{}, err
	}
	stats, err := s.store.DeckCardStats(ctx, deckID)
	if err != nil {
		return nil, DeckStats{}, err
	}
	return card, DeckStats{
		Active:  stats.Active,
		Snoozed: stats.Snoozed,
		Total:   stats.Total,
	}, nil
}

func parseCardStatus(value string) (domain.CardStatus, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "active":
		return domain.CardStatusActive, nil
	case "snoozed":
		return domain.CardStatusSnoozed, nil
	case "removed":
		return domain.CardStatusRemoved, nil
	default:
		return "", fmt.Errorf("--status must be one of: active, snoozed, removed")
	}
}
