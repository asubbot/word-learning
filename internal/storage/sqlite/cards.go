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
	query := `SELECT id, deck_id, front, back, pronunciation, description, status, snoozed_until, next_due_at, interval_sec, ease, lapses, last_reviewed_at FROM cards WHERE deck_id = ?`
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

func (s *Store) GetCardByID(ctx context.Context, cardID int64) (*domain.Card, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, deck_id, front, back, pronunciation, description, status, snoozed_until, next_due_at, interval_sec, ease, lapses, last_reviewed_at
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

func (s *Store) SetCardStatus(ctx context.Context, cardID int64, status domain.CardStatus, snoozedUntil *time.Time) (bool, error) {
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE cards SET status = ?, snoozed_until = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		string(status),
		snoozedUntil,
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
		 SET status = 'active', snoozed_until = NULL, next_due_at = ?, updated_at = CURRENT_TIMESTAMP
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
		     snoozed_until = NULL,
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
		`SELECT id, deck_id, front, back, pronunciation, description, status, snoozed_until, next_due_at, interval_sec, ease, lapses, last_reviewed_at
		 FROM cards
		 WHERE deck_id = ?
		   AND status != 'removed'
		   AND (
		     (status = 'active' AND next_due_at <= ?)
		     OR (status = 'snoozed' AND (snoozed_until IS NULL OR snoozed_until <= ?))
		   )
		 ORDER BY COALESCE(next_due_at, snoozed_until, created_at) ASC, id ASC
		 LIMIT 1`,
		deckID,
		now.UTC(),
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
			COALESCE(SUM(CASE WHEN (status = 'active' AND next_due_at > ?) OR status = 'snoozed' THEN 1 ELSE 0 END), 0),
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

func scanCard(scanner interface{ Scan(dest ...any) error }) (domain.Card, error) {
	var card domain.Card
	var status string
	var snoozedUntil sql.NullTime
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
		&snoozedUntil,
		&nextDueAt,
		&card.IntervalSec,
		&card.Ease,
		&card.Lapses,
		&lastReviewedAt,
	); err != nil {
		return domain.Card{}, fmt.Errorf("scan card row: %w", err)
	}
	card.Status = domain.CardStatus(status)
	if snoozedUntil.Valid {
		card.SnoozedUntil = &snoozedUntil.Time
	}
	if lastReviewedAt.Valid {
		card.LastReviewedAt = &lastReviewedAt.Time
	}
	card.NextDueAt = nextDueAt
	return card, nil
}
