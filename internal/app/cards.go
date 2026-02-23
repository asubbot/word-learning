package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"word-learning-cli/internal/domain"
	"word-learning-cli/internal/storage/sqlite"
)

var ErrCardNotFound = errors.New("card not found")

type DeckStats struct {
	Active    int64
	Postponed int64
	Total     int64
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

	updated, err := s.store.SetCardStatus(ctx, cardID, domain.CardStatusRemoved)
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

	updated, err := s.store.SetCardActiveNow(ctx, cardID, time.Now().UTC())
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

	card, err := s.store.GetCardByID(ctx, cardID)
	if err != nil {
		return err
	}
	if card == nil {
		return ErrCardNotFound
	}

	now := time.Now().UTC()
	intervalSec := nextRememberIntervalSec(card.IntervalSec, card.Ease)
	ease := math.Min(2.8, maxEase(card.Ease)+0.05)

	updated, err := s.store.UpdateCardSchedule(
		ctx,
		cardID,
		now.Add(time.Duration(intervalSec)*time.Second),
		intervalSec,
		ease,
		card.Lapses,
		now,
	)
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

	card, err := s.store.GetCardByID(ctx, cardID)
	if err != nil {
		return err
	}
	if card == nil {
		return ErrCardNotFound
	}

	now := time.Now().UTC()
	lapses := card.Lapses + 1
	ease := math.Max(1.3, maxEase(card.Ease)-0.2)
	const shortIntervalSec int64 = 600

	updated, err := s.store.UpdateCardSchedule(
		ctx,
		cardID,
		now.Add(time.Duration(shortIntervalSec)*time.Second),
		shortIntervalSec,
		ease,
		lapses,
		now,
	)
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
	now := time.Now().UTC()
	stats, err := s.store.DeckCardStats(ctx, deckID, now)
	if err != nil {
		return nil, DeckStats{}, err
	}
	return card, DeckStats{
		Active:    stats.Active,
		Postponed: stats.Postponed,
		Total:     stats.Total,
	}, nil
}

func parseCardStatus(value string) (domain.CardStatus, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "active":
		return domain.CardStatusActive, nil
	case "removed":
		return domain.CardStatusRemoved, nil
	default:
		return "", fmt.Errorf("--status must be one of: active, removed")
	}
}

func nextRememberIntervalSec(currentInterval int64, ease float64) int64 {
	const oneDaySec int64 = 86400
	if currentInterval <= 0 {
		return oneDaySec
	}
	grown := int64(math.Round(float64(currentInterval) * maxEase(ease)))
	if grown < oneDaySec {
		return oneDaySec
	}
	return grown
}

func maxEase(value float64) float64 {
	if value <= 0 {
		return 2.5
	}
	return value
}
