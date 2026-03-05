package sqlite

import (
	"context"
	"fmt"
	"word-learning/internal/domain"
	"word-learning/internal/export"
)

// ExportDeckForOwner returns the deck and its active cards for export.
// Returns error if deck is not owned by the user.
func (s *Store) ExportDeckForOwner(ctx context.Context, deckID, telegramUserID int64) (deck domain.Deck, cards []domain.Card, err error) {
	d, err := s.GetDeckForOwner(ctx, deckID, telegramUserID)
	if err != nil {
		return domain.Deck{}, nil, err
	}
	if d == nil {
		return domain.Deck{}, nil, fmt.Errorf("deck %d not found", deckID)
	}
	activeStatus := domain.CardStatusActive
	cards, err = s.ListCardsForOwner(ctx, deckID, telegramUserID, &activeStatus)
	if err != nil {
		return domain.Deck{}, nil, err
	}
	return *d, cards, nil
}

// ImportDeckForOwner creates a new deck for the owner and imports cards.
// Returns the created deck and the number of cards imported.
func (s *Store) ImportDeckForOwner(ctx context.Context, telegramUserID int64, name, langFrom, langTo string, cardContents []export.CardContent) (deck domain.Deck, importedCount int, err error) {
	deck, err = s.CreateDeckForOwner(ctx, telegramUserID, name, langFrom, langTo)
	if err != nil {
		return domain.Deck{}, 0, err
	}
	for _, c := range cardContents {
		_, err = s.CreateCard(ctx, CardCreateParams{
			DeckID:        deck.ID,
			Front:         c.Front,
			Back:          c.Back,
			Pronunciation: c.Pronunciation,
			Example:       c.Example,
			Conjugation:   c.Conjugation,
		})
		if err != nil {
			return deck, importedCount, fmt.Errorf("import card %q: %w", c.Front, err)
		}
		importedCount++
	}
	return deck, importedCount, nil
}

// InsertCardsToDeckForOwner inserts card contents into an existing deck (no duplicate checks).
// The app layer uses ImportCardsToDeckForUser (via AddCardForUser) for user-facing import
// to handle duplicates. This method is a low-level primitive for tests and bulk operations.
func (s *Store) InsertCardsToDeckForOwner(ctx context.Context, deckID, telegramUserID int64, cards []export.CardContent) (int, error) {
	deck, err := s.GetDeckForOwner(ctx, deckID, telegramUserID)
	if err != nil {
		return 0, err
	}
	if deck == nil {
		return 0, fmt.Errorf("deck %d not found", deckID)
	}
	importedCount := 0
	for _, c := range cards {
		_, err = s.CreateCard(ctx, CardCreateParams{
			DeckID:        deck.ID,
			Front:         c.Front,
			Back:          c.Back,
			Pronunciation: c.Pronunciation,
			Example:       c.Example,
			Conjugation:   c.Conjugation,
		})
		if err != nil {
			return importedCount, fmt.Errorf("import card %q: %w", c.Front, err)
		}
		importedCount++
	}
	return importedCount, nil
}
