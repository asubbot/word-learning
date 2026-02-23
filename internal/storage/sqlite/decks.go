package sqlite

import (
	"context"
	"fmt"
	"strings"

	"word-learning-cli/internal/domain"
)

func (s *Store) CreateDeck(ctx context.Context, name, languageFrom, languageTo string) (domain.Deck, error) {
	return s.CreateDeckForOwner(ctx, 0, name, languageFrom, languageTo)
}

func (s *Store) CreateDeckForOwner(ctx context.Context, telegramUserID int64, name, languageFrom, languageTo string) (domain.Deck, error) {
	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO decks (telegram_user_id, name, language_from, language_to) VALUES (?, ?, ?, ?)`,
		telegramUserID,
		name,
		languageFrom,
		languageTo,
	)
	if err != nil {
		return domain.Deck{}, fmt.Errorf("insert deck: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return domain.Deck{}, fmt.Errorf("get new deck id: %w", err)
	}

	return domain.Deck{
		TelegramUserID: telegramUserID,
		ID:             id,
		Name:           name,
		LanguageFrom:   languageFrom,
		LanguageTo:     languageTo,
	}, nil
}

func (s *Store) ListDecks(ctx context.Context) (decks []domain.Deck, err error) {
	return s.ListDecksForOwner(ctx, 0)
}

func (s *Store) ListDecksForOwner(ctx context.Context, telegramUserID int64) (decks []domain.Deck, err error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT telegram_user_id, id, name, language_from, language_to
		 FROM decks
		 WHERE telegram_user_id = ?
		 ORDER BY id ASC`,
		telegramUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("list decks: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close deck rows: %w", closeErr)
		}
	}()

	decks = make([]domain.Deck, 0)
	for rows.Next() {
		var d domain.Deck
		if err := rows.Scan(&d.TelegramUserID, &d.ID, &d.Name, &d.LanguageFrom, &d.LanguageTo); err != nil {
			return nil, fmt.Errorf("scan deck row: %w", err)
		}
		d.Name = strings.TrimSpace(d.Name)
		decks = append(decks, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate decks: %w", err)
	}

	return decks, nil
}

func (s *Store) DeckExistsForOwner(ctx context.Context, deckID int64, telegramUserID int64) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM decks WHERE id = ? AND telegram_user_id = ?)`,
		deckID,
		telegramUserID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check owner deck exists: %w", err)
	}
	return exists == 1, nil
}
