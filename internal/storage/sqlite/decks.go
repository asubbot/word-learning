package sqlite

import (
	"context"
	"database/sql"
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

func (s *Store) ListDecksAll(ctx context.Context) (decks []domain.Deck, err error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT telegram_user_id, id, name, language_from, language_to, created_at, updated_at
		 FROM decks
		 ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list all decks: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close deck rows: %w", closeErr)
		}
	}()

	decks = make([]domain.Deck, 0)
	for rows.Next() {
		var d domain.Deck
		if err := rows.Scan(&d.TelegramUserID, &d.ID, &d.Name, &d.LanguageFrom, &d.LanguageTo, &d.CreatedAt, &d.UpdatedAt); err != nil {
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

func (s *Store) GetDeckByID(ctx context.Context, deckID int64) (*domain.Deck, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT telegram_user_id, id, name, language_from, language_to, created_at, updated_at
		 FROM decks
		 WHERE id = ?`,
		deckID,
	)
	var d domain.Deck
	if err := row.Scan(&d.TelegramUserID, &d.ID, &d.Name, &d.LanguageFrom, &d.LanguageTo, &d.CreatedAt, &d.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get deck by id: %w", err)
	}
	d.Name = strings.TrimSpace(d.Name)
	return &d, nil
}

func (s *Store) ListDecksForOwner(ctx context.Context, telegramUserID int64) (decks []domain.Deck, err error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT telegram_user_id, id, name, language_from, language_to, created_at, updated_at
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
		if err := rows.Scan(&d.TelegramUserID, &d.ID, &d.Name, &d.LanguageFrom, &d.LanguageTo, &d.CreatedAt, &d.UpdatedAt); err != nil {
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

func (s *Store) GetDeckForOwner(ctx context.Context, deckID int64, telegramUserID int64) (*domain.Deck, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT telegram_user_id, id, name, language_from, language_to, created_at, updated_at
		 FROM decks
		 WHERE id = ? AND telegram_user_id = ?`,
		deckID,
		telegramUserID,
	)
	var d domain.Deck
	if err := row.Scan(&d.TelegramUserID, &d.ID, &d.Name, &d.LanguageFrom, &d.LanguageTo, &d.CreatedAt, &d.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get deck: %w", err)
	}
	d.Name = strings.TrimSpace(d.Name)
	return &d, nil
}

func (s *Store) SetActiveDeckForUser(ctx context.Context, userID, deckID int64) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO active_decks (user_id, deck_id, updated_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(user_id) DO UPDATE
		 SET deck_id = excluded.deck_id, updated_at = CURRENT_TIMESTAMP`,
		userID,
		deckID,
	)
	if err != nil {
		return fmt.Errorf("set active deck: %w", err)
	}
	return nil
}

func (s *Store) ClearActiveDeckForUser(ctx context.Context, userID int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM active_decks WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("clear active deck: %w", err)
	}
	return nil
}

func (s *Store) GetActiveDeckForUser(ctx context.Context, userID int64) (*domain.Deck, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT d.telegram_user_id, d.id, d.name, d.language_from, d.language_to, d.created_at, d.updated_at
		 FROM active_decks a
		 INNER JOIN decks d ON d.id = a.deck_id
		 WHERE a.user_id = ?`,
		userID,
	)
	var d domain.Deck
	if err := row.Scan(&d.TelegramUserID, &d.ID, &d.Name, &d.LanguageFrom, &d.LanguageTo, &d.CreatedAt, &d.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get active deck: %w", err)
	}
	d.Name = strings.TrimSpace(d.Name)
	return &d, nil
}

func (s *Store) FindDeckByExactNameForOwner(ctx context.Context, telegramUserID int64, name string) (*domain.Deck, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT telegram_user_id, id, name, language_from, language_to, created_at, updated_at
		 FROM decks
		 WHERE telegram_user_id = ?
		   AND lower(trim(name)) = lower(trim(?))
		 ORDER BY id ASC
		 LIMIT 1`,
		telegramUserID,
		name,
	)
	var d domain.Deck
	if err := row.Scan(&d.TelegramUserID, &d.ID, &d.Name, &d.LanguageFrom, &d.LanguageTo, &d.CreatedAt, &d.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("find deck by exact name: %w", err)
	}
	d.Name = strings.TrimSpace(d.Name)
	return &d, nil
}

func (s *Store) FindDeckCandidatesForOwner(ctx context.Context, telegramUserID int64, name string, limit int) (decks []domain.Deck, err error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return []domain.Deck{}, nil
	}
	if limit <= 0 {
		limit = 10
	}
	pattern := "%" + strings.ToLower(trimmed) + "%"
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT telegram_user_id, id, name, language_from, language_to, created_at, updated_at
		 FROM decks
		 WHERE telegram_user_id = ?
		   AND lower(name) LIKE ?
		 ORDER BY id ASC
		 LIMIT ?`,
		telegramUserID,
		pattern,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("find deck candidates: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close candidate deck rows: %w", closeErr)
		}
	}()

	decks = make([]domain.Deck, 0)
	for rows.Next() {
		var d domain.Deck
		if scanErr := rows.Scan(&d.TelegramUserID, &d.ID, &d.Name, &d.LanguageFrom, &d.LanguageTo, &d.CreatedAt, &d.UpdatedAt); scanErr != nil {
			return nil, fmt.Errorf("scan candidate deck row: %w", scanErr)
		}
		d.Name = strings.TrimSpace(d.Name)
		decks = append(decks, d)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate candidate decks: %w", rowsErr)
	}
	return decks, nil
}
