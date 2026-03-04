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
var ErrCardAlreadyExists = errors.New("card with this front already exists in deck")

type DeckStats struct {
	Active    int64
	Postponed int64
	Total     int64
}

func (s *Service) AddCard(ctx context.Context, deckID int64, front, back, pronunciation, example, conjugation string) (domain.Card, error) {
	return s.AddCardForUser(ctx, 0, deckID, front, back, pronunciation, example, conjugation)
}

func (s *Service) AddCardForActiveDeckForUser(ctx context.Context, telegramUserID int64, front, back, pronunciation, example, conjugation string) (domain.Card, error) {
	deck, err := s.ResolveActiveDeckForUser(ctx, telegramUserID)
	if err != nil {
		return domain.Card{}, err
	}
	return s.AddCardForUser(ctx, telegramUserID, deck.ID, front, back, pronunciation, example, conjugation)
}

func (s *Service) AddCardToDeck(ctx context.Context, deckID int64, front, back, pronunciation, example, conjugation string) (domain.Card, error) {
	deck, err := s.store.GetDeckByID(ctx, deckID)
	if err != nil {
		return domain.Card{}, err
	}
	if deck == nil {
		return domain.Card{}, fmt.Errorf("deck %d does not exist", deckID)
	}
	return s.AddCardForUser(ctx, deck.TelegramUserID, deckID, front, back, pronunciation, example, conjugation)
}

func (s *Service) AddCardForUser(ctx context.Context, telegramUserID, deckID int64, front, back, pronunciation, example, conjugation string) (domain.Card, error) {
	if deckID <= 0 {
		return domain.Card{}, fmt.Errorf("--deck must be a positive integer")
	}

	front = strings.TrimSpace(front)
	back = strings.TrimSpace(back)
	pronunciation = strings.TrimSpace(pronunciation)
	example = strings.TrimSpace(example)
	conjugation = strings.TrimSpace(conjugation)
	if front == "" {
		return domain.Card{}, fmt.Errorf("front must not be empty")
	}
	if back == "" {
		return domain.Card{}, fmt.Errorf("back must not be empty")
	}

	exists, err := s.store.DeckExistsForOwner(ctx, deckID, telegramUserID)
	if err != nil {
		return domain.Card{}, err
	}
	if !exists {
		return domain.Card{}, fmt.Errorf("deck %d does not exist", deckID)
	}

	frontExists, err := s.store.CardFrontExistsInDeckForOwner(ctx, deckID, telegramUserID, front)
	if err != nil {
		return domain.Card{}, err
	}
	if frontExists {
		return domain.Card{}, ErrCardAlreadyExists
	}

	return s.store.CreateCard(ctx, sqlite.CardCreateParams{
		DeckID:        deckID,
		Front:         front,
		Back:          back,
		Pronunciation: pronunciation,
		Example:       example,
		Conjugation:   conjugation,
	})
}

func (s *Service) ListCards(ctx context.Context, deckID int64, status string) ([]domain.Card, error) {
	return s.ListCardsForUser(ctx, 0, deckID, status)
}

func (s *Service) ListCardsForActiveDeckForUser(ctx context.Context, telegramUserID int64, status string) ([]domain.Card, error) {
	deck, err := s.ResolveActiveDeckForUser(ctx, telegramUserID)
	if err != nil {
		return nil, err
	}
	return s.ListCardsForUser(ctx, telegramUserID, deck.ID, status)
}

func (s *Service) ListCardsForUser(ctx context.Context, telegramUserID, deckID int64, status string) ([]domain.Card, error) {
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

	return s.store.ListCardsForOwner(ctx, deckID, telegramUserID, statusPtr)
}

func (s *Service) ListCardsInDeck(ctx context.Context, deckID int64, status string) ([]domain.Card, error) {
	deck, err := s.store.GetDeckByID(ctx, deckID)
	if err != nil {
		return nil, err
	}
	if deck == nil {
		return nil, fmt.Errorf("deck %d does not exist", deckID)
	}
	return s.ListCardsForUser(ctx, deck.TelegramUserID, deckID, status)
}

func (s *Service) RemoveCard(ctx context.Context, cardID int64) error {
	return s.RemoveCardForUser(ctx, 0, cardID)
}

// resolveCardAndDeck fetches the card by ID and its deck; returns ErrCardNotFound or deck-not-found error if missing.
func (s *Service) resolveCardAndDeck(ctx context.Context, cardID int64) (*domain.Card, *domain.Deck, error) {
	card, err := s.store.GetCardByID(ctx, cardID)
	if err != nil {
		return nil, nil, err
	}
	if card == nil {
		return nil, nil, ErrCardNotFound
	}
	deck, err := s.store.GetDeckByID(ctx, card.DeckID)
	if err != nil {
		return nil, nil, err
	}
	if deck == nil {
		return nil, nil, fmt.Errorf("deck %d not found", card.DeckID)
	}
	return card, deck, nil
}

func (s *Service) RemoveCardByID(ctx context.Context, cardID int64) error {
	_, deck, err := s.resolveCardAndDeck(ctx, cardID)
	if err != nil {
		return err
	}
	return s.RemoveCardForUser(ctx, deck.TelegramUserID, cardID)
}

func (s *Service) RestoreCardByID(ctx context.Context, cardID int64) error {
	_, deck, err := s.resolveCardAndDeck(ctx, cardID)
	if err != nil {
		return err
	}
	return s.RestoreCardForUser(ctx, deck.TelegramUserID, cardID)
}

func (s *Service) RememberCardByID(ctx context.Context, cardID int64) error {
	_, deck, err := s.resolveCardAndDeck(ctx, cardID)
	if err != nil {
		return err
	}
	return s.RememberCardForUser(ctx, deck.TelegramUserID, cardID)
}

func (s *Service) DontRememberCardByID(ctx context.Context, cardID int64) error {
	_, deck, err := s.resolveCardAndDeck(ctx, cardID)
	if err != nil {
		return err
	}
	return s.DontRememberCardForUser(ctx, deck.TelegramUserID, cardID)
}

func (s *Service) NextCardWithStatsInDeck(ctx context.Context, deckID int64) (*domain.Card, DeckStats, error) {
	deck, err := s.store.GetDeckByID(ctx, deckID)
	if err != nil {
		return nil, DeckStats{}, err
	}
	if deck == nil {
		return nil, DeckStats{}, fmt.Errorf("deck %d does not exist", deckID)
	}
	return s.NextCardWithStatsForUser(ctx, deck.TelegramUserID, deckID)
}

func (s *Service) RemoveCardForUser(ctx context.Context, telegramUserID, cardID int64) error {
	if cardID <= 0 {
		return fmt.Errorf("--id must be a positive integer")
	}

	if _, err := s.mustCardForUser(ctx, telegramUserID, cardID); err != nil {
		return err
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
	return s.RestoreCardForUser(ctx, 0, cardID)
}

func (s *Service) RestoreCardForUser(ctx context.Context, telegramUserID, cardID int64) error {
	if cardID <= 0 {
		return fmt.Errorf("--id must be a positive integer")
	}

	if _, err := s.mustCardForUser(ctx, telegramUserID, cardID); err != nil {
		return err
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
	return s.RememberCardForUser(ctx, 0, cardID)
}

func (s *Service) RememberCardForUser(ctx context.Context, telegramUserID, cardID int64) error {
	if cardID <= 0 {
		return fmt.Errorf("--id must be a positive integer")
	}

	card, err := s.mustCardForUser(ctx, telegramUserID, cardID)
	if err != nil {
		return err
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
	return s.DontRememberCardForUser(ctx, 0, cardID)
}

func (s *Service) DontRememberCardForUser(ctx context.Context, telegramUserID, cardID int64) error {
	if cardID <= 0 {
		return fmt.Errorf("--id must be a positive integer")
	}

	card, err := s.mustCardForUser(ctx, telegramUserID, cardID)
	if err != nil {
		return err
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
	return s.NextCardForUser(ctx, 0, deckID)
}

func (s *Service) NextCardWithStatsForActiveDeckForUser(ctx context.Context, telegramUserID int64) (*domain.Card, DeckStats, error) {
	deck, err := s.ResolveActiveDeckForUser(ctx, telegramUserID)
	if err != nil {
		return nil, DeckStats{}, err
	}
	return s.NextCardWithStatsForUser(ctx, telegramUserID, deck.ID)
}

func (s *Service) NextCardForUser(ctx context.Context, telegramUserID, deckID int64) (*domain.Card, error) {
	if deckID <= 0 {
		return nil, fmt.Errorf("--deck must be a positive integer")
	}
	exists, err := s.store.DeckExistsForOwner(ctx, deckID, telegramUserID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("deck %d does not exist", deckID)
	}
	return s.store.NextCardForDeckForOwner(ctx, deckID, telegramUserID, time.Now().UTC())
}

func (s *Service) NextCardWithStats(ctx context.Context, deckID int64) (*domain.Card, DeckStats, error) {
	return s.NextCardWithStatsForUser(ctx, 0, deckID)
}

func (s *Service) NextCardWithStatsForUser(ctx context.Context, telegramUserID, deckID int64) (*domain.Card, DeckStats, error) {
	card, err := s.NextCardForUser(ctx, telegramUserID, deckID)
	if err != nil {
		return nil, DeckStats{}, err
	}
	now := time.Now().UTC()
	stats, err := s.store.DeckCardStatsForOwner(ctx, deckID, telegramUserID, now)
	if err != nil {
		return nil, DeckStats{}, err
	}
	return card, DeckStats{
		Active:    stats.Active,
		Postponed: stats.Postponed,
		Total:     stats.Total,
	}, nil
}

func (s *Service) GetCardByIDForUser(ctx context.Context, telegramUserID, cardID int64) (*domain.Card, error) {
	if cardID <= 0 {
		return nil, fmt.Errorf("--id must be a positive integer")
	}
	return s.mustCardForUser(ctx, telegramUserID, cardID)
}

func (s *Service) mustCardForUser(ctx context.Context, telegramUserID, cardID int64) (*domain.Card, error) {
	card, err := s.store.GetCardByIDForOwner(ctx, cardID, telegramUserID)
	if err != nil {
		return nil, err
	}
	if card == nil {
		return nil, ErrCardNotFound
	}
	return card, nil
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
