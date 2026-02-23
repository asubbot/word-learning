package sqlite

import (
	"context"
	"fmt"
	"strings"

	"word-learning-cli/internal/domain"
)

func (s *Store) CreateDeck(ctx context.Context, name, languageFrom, languageTo string) (domain.Deck, error) {
	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO decks (name, language_from, language_to) VALUES (?, ?, ?)`,
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
		ID:           id,
		Name:         name,
		LanguageFrom: languageFrom,
		LanguageTo:   languageTo,
	}, nil
}

func (s *Store) ListDecks(ctx context.Context) ([]domain.Deck, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, language_from, language_to FROM decks ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list decks: %w", err)
	}
	defer rows.Close()

	decks := make([]domain.Deck, 0)
	for rows.Next() {
		var d domain.Deck
		if err := rows.Scan(&d.ID, &d.Name, &d.LanguageFrom, &d.LanguageTo); err != nil {
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
