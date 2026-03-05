package app

import (
	"context"
	"errors"
	"fmt"
	"word-learning/internal/domain"
	"word-learning/internal/export"
)

// ImportReport holds per-card import statistics.
type ImportReport struct {
	Total             int
	Created           int
	SkippedDuplicates int
	Failed            int
}

// ExportDeckForUser exports a deck owned by the user as JSON.
func (s *Service) ExportDeckForUser(ctx context.Context, telegramUserID, deckID int64) ([]byte, error) {
	if deckID <= 0 {
		return nil, fmt.Errorf("deck id must be a positive integer")
	}
	exists, err := s.store.DeckExistsForOwner(ctx, deckID, telegramUserID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("deck %d not found", deckID)
	}
	deck, cards, err := s.store.ExportDeckForOwner(ctx, deckID, telegramUserID)
	if err != nil {
		return nil, err
	}
	return export.MarshalExport(deck, cards)
}

// CreateDeckFromExportForUser creates a new deck from export JSON and imports all cards.
// Use ImportCardsToDeckForUser when adding cards to an existing deck.
func (s *Service) CreateDeckFromExportForUser(ctx context.Context, telegramUserID int64, data []byte) (deck domain.Deck, importedCount int, err error) {
	exp, err := export.UnmarshalExport(data)
	if err != nil {
		return domain.Deck{}, 0, err
	}
	normalizedFrom, err := normalizeLanguageCode(exp.Deck.LanguageFrom)
	if err != nil {
		return domain.Deck{}, 0, fmt.Errorf("invalid language_from: %w", err)
	}
	normalizedTo, err := normalizeLanguageCode(exp.Deck.LanguageTo)
	if err != nil {
		return domain.Deck{}, 0, fmt.Errorf("invalid language_to: %w", err)
	}
	return s.store.ImportDeckForOwner(ctx, telegramUserID, exp.Deck.Name, normalizedFrom, normalizedTo, exp.Cards)
}

// ImportCardsToDeckForUser parses export data and adds cards to an existing deck.
// Verifies deck ownership and that deck language pair matches the export.
// Returns ImportReport with created/skipped_duplicates/failed counts.
func (s *Service) ImportCardsToDeckForUser(ctx context.Context, telegramUserID, deckID int64, data []byte) (ImportReport, error) {
	exp, err := export.UnmarshalExport(data)
	if err != nil {
		return ImportReport{}, err
	}
	normalizedFrom, err := normalizeLanguageCode(exp.Deck.LanguageFrom)
	if err != nil {
		return ImportReport{}, fmt.Errorf("invalid language_from: %w", err)
	}
	normalizedTo, err := normalizeLanguageCode(exp.Deck.LanguageTo)
	if err != nil {
		return ImportReport{}, fmt.Errorf("invalid language_to: %w", err)
	}
	deck, err := s.store.GetDeckForOwner(ctx, deckID, telegramUserID)
	if err != nil {
		return ImportReport{}, err
	}
	if deck == nil {
		return ImportReport{}, fmt.Errorf("deck %d not found", deckID)
	}
	if deck.LanguageFrom != normalizedFrom || deck.LanguageTo != normalizedTo {
		return ImportReport{}, fmt.Errorf("deck language pair (%s->%s) does not match import (%s->%s)",
			deck.LanguageFrom, deck.LanguageTo, normalizedFrom, normalizedTo)
	}
	report := ImportReport{Total: len(exp.Cards)}
	for _, c := range exp.Cards {
		_, addErr := s.AddCardForUser(ctx, telegramUserID, deckID, c.Front, c.Back, c.Pronunciation, c.Example, c.Conjugation)
		if addErr == nil {
			report.Created++
			continue
		}
		if errors.Is(addErr, ErrCardAlreadyExists) {
			report.SkippedDuplicates++
			continue
		}
		report.Failed++
	}
	return report, nil
}
