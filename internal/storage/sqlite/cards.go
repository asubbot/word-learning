package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"word-learning-cli/internal/domain"
)

type CardCreateParams struct {
	DeckID        int64
	Front         string
	Back          string
	Pronunciation string
	Description   string
}

type DeckCardStats struct {
	Active    int64
	Postponed int64
	Total     int64
}

func (s *Store) DeckExists(ctx context.Context, deckID int64) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM decks WHERE id = ?)`, deckID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check deck exists: %w", err)
	}
	return exists == 1, nil
}

func (s *Store) CardFrontExistsInDeckForOwner(ctx context.Context, deckID int64, telegramUserID int64, front string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(
			SELECT 1
			FROM cards c
			INNER JOIN decks d ON d.id = c.deck_id
			WHERE c.deck_id = ?
			  AND d.telegram_user_id = ?
			  AND lower(trim(c.front)) = lower(trim(?))
		)`,
		deckID,
		telegramUserID,
		front,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check card front exists: %w", err)
	}
	return exists == 1, nil
}

func (s *Store) CreateCard(ctx context.Context, params CardCreateParams) (domain.Card, error) {
	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO cards (deck_id, front, back, pronunciation, description, status) VALUES (?, ?, ?, ?, ?, 'active')`,
		params.DeckID,
		params.Front,
		params.Back,
		params.Pronunciation,
		params.Description,
	)
	if err != nil {
		return domain.Card{}, fmt.Errorf("insert card: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return domain.Card{}, fmt.Errorf("get new card id: %w", err)
	}

	card, err := s.GetCardByID(ctx, id)
	if err != nil {
		return domain.Card{}, err
	}
	if card == nil {
		return domain.Card{}, fmt.Errorf("created card %d not found", id)
	}
	return *card, nil
}

func (s *Store) ListCards(ctx context.Context, deckID int64, status *domain.CardStatus) (cards []domain.Card, err error) {
	query := `SELECT id, deck_id, front, back, pronunciation, description, status, next_due_at, interval_sec, ease, lapses, last_reviewed_at FROM cards WHERE deck_id = ?`
	args := []any{deckID}
	if status != nil {
		query += ` AND status = ?`
		args = append(args, string(*status))
	}
	query += ` ORDER BY id ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close card rows: %w", closeErr)
		}
	}()

	cards = make([]domain.Card, 0)
	for rows.Next() {
		card, scanErr := scanCard(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		cards = append(cards, card)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate cards: %w", rowsErr)
	}

	return cards, nil
}

func (s *Store) ListCardsForOwner(ctx context.Context, deckID int64, telegramUserID int64, status *domain.CardStatus) (cards []domain.Card, err error) {
	query := `SELECT c.id, c.deck_id, c.front, c.back, c.pronunciation, c.description, c.status, c.next_due_at, c.interval_sec, c.ease, c.lapses, c.last_reviewed_at
		FROM cards c
		INNER JOIN decks d ON d.id = c.deck_id
		WHERE c.deck_id = ? AND d.telegram_user_id = ?`
	args := []any{deckID, telegramUserID}
	if status != nil {
		query += ` AND c.status = ?`
		args = append(args, string(*status))
	}
	query += ` ORDER BY c.id ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list owner cards: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close owner card rows: %w", closeErr)
		}
	}()

	cards = make([]domain.Card, 0)
	for rows.Next() {
		card, scanErr := scanCard(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		cards = append(cards, card)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate owner cards: %w", rowsErr)
	}
	return cards, nil
}

func (s *Store) GetCardByID(ctx context.Context, cardID int64) (*domain.Card, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, deck_id, front, back, pronunciation, description, status, next_due_at, interval_sec, ease, lapses, last_reviewed_at
		 FROM cards
		 WHERE id = ?`,
		cardID,
	)
	card, err := scanCard(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &card, nil
}

func (s *Store) GetCardByIDForOwner(ctx context.Context, cardID int64, telegramUserID int64) (*domain.Card, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT c.id, c.deck_id, c.front, c.back, c.pronunciation, c.description, c.status, c.next_due_at, c.interval_sec, c.ease, c.lapses, c.last_reviewed_at
		 FROM cards c
		 INNER JOIN decks d ON d.id = c.deck_id
		 WHERE c.id = ? AND d.telegram_user_id = ?`,
		cardID,
		telegramUserID,
	)
	card, err := scanCard(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &card, nil
}

func (s *Store) SetCardStatus(ctx context.Context, cardID int64, status domain.CardStatus) (bool, error) {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE cards SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		string(status),
		cardID,
	)
	if err != nil {
		return false, fmt.Errorf("update card status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count updated rows: %w", err)
	}

	return rows > 0, nil
}

func (s *Store) SetCardActiveNow(ctx context.Context, cardID int64, now time.Time) (bool, error) {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE cards
		 SET status = 'active', next_due_at = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		now.UTC(),
		cardID,
	)
	if err != nil {
		return false, fmt.Errorf("set card active now: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count updated rows: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) UpdateCardSchedule(ctx context.Context, cardID int64, nextDueAt time.Time, intervalSec int64, ease float64, lapses int64, lastReviewedAt time.Time) (bool, error) {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE cards
		 SET status = 'active',
		     next_due_at = ?,
		     interval_sec = ?,
		     ease = ?,
		     lapses = ?,
		     last_reviewed_at = ?,
		     updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		nextDueAt.UTC(),
		intervalSec,
		ease,
		lapses,
		lastReviewedAt.UTC(),
		cardID,
	)
	if err != nil {
		return false, fmt.Errorf("update card schedule: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count updated rows: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) NextCardForDeck(ctx context.Context, deckID int64, now time.Time) (*domain.Card, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, deck_id, front, back, pronunciation, description, status, next_due_at, interval_sec, ease, lapses, last_reviewed_at
		 FROM cards
		 WHERE deck_id = ?
		   AND status = 'active'
		   AND next_due_at <= ?
		 ORDER BY next_due_at ASC, id ASC
		 LIMIT 1`,
		deckID,
		now.UTC(),
	)

	card, err := scanCard(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &card, nil
}

func (s *Store) NextCardForDeckForOwner(ctx context.Context, deckID int64, telegramUserID int64, now time.Time) (*domain.Card, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT c.id, c.deck_id, c.front, c.back, c.pronunciation, c.description, c.status, c.next_due_at, c.interval_sec, c.ease, c.lapses, c.last_reviewed_at
		 FROM cards c
		 INNER JOIN decks d ON d.id = c.deck_id
		 WHERE c.deck_id = ?
		   AND d.telegram_user_id = ?
		   AND c.status = 'active'
		   AND c.next_due_at <= ?
		 ORDER BY c.next_due_at ASC, c.id ASC
		 LIMIT 1`,
		deckID,
		telegramUserID,
		now.UTC(),
	)
	card, err := scanCard(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &card, nil
}

func (s *Store) DeckCardStats(ctx context.Context, deckID int64, now time.Time) (DeckCardStats, error) {
	var stats DeckCardStats
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT
			COALESCE(SUM(CASE WHEN status = 'active' AND next_due_at <= ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'active' AND next_due_at > ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status != 'removed' THEN 1 ELSE 0 END), 0)
		 FROM cards
		 WHERE deck_id = ?`,
		now.UTC(),
		now.UTC(),
		deckID,
	).Scan(&stats.Active, &stats.Postponed, &stats.Total); err != nil {
		return DeckCardStats{}, fmt.Errorf("get deck card stats: %w", err)
	}
	return stats, nil
}

func (s *Store) DeckCardStatsForOwner(ctx context.Context, deckID int64, telegramUserID int64, now time.Time) (DeckCardStats, error) {
	var stats DeckCardStats
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT
			COALESCE(SUM(CASE WHEN c.status = 'active' AND c.next_due_at <= ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN c.status = 'active' AND c.next_due_at > ? THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN c.status != 'removed' THEN 1 ELSE 0 END), 0)
		 FROM cards c
		 INNER JOIN decks d ON d.id = c.deck_id
		 WHERE c.deck_id = ? AND d.telegram_user_id = ?`,
		now.UTC(),
		now.UTC(),
		deckID,
		telegramUserID,
	).Scan(&stats.Active, &stats.Postponed, &stats.Total); err != nil {
		return DeckCardStats{}, fmt.Errorf("get owner deck card stats: %w", err)
	}
	return stats, nil
}

func scanCard(scanner interface{ Scan(dest ...any) error }) (domain.Card, error) {
	var card domain.Card
	var status string
	var nextDueAt time.Time
	var lastReviewedAt sql.NullTime

	if err := scanner.Scan(
		&card.ID,
		&card.DeckID,
		&card.Front,
		&card.Back,
		&card.Pronunciation,
		&card.Description,
		&status,
		&nextDueAt,
		&card.IntervalSec,
		&card.Ease,
		&card.Lapses,
		&lastReviewedAt,
	); err != nil {
		return domain.Card{}, fmt.Errorf("scan card row: %w", err)
	}
	card.Status = domain.CardStatus(status)
	if lastReviewedAt.Valid {
		card.LastReviewedAt = &lastReviewedAt.Time
	}
	card.NextDueAt = nextDueAt
	return card, nil
}
