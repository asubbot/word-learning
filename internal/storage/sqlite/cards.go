package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"word-learning-cli/internal/domain"
)

type CardCreateParams struct {
	DeckID        int64
	Front         string
	Back          string
	Pronunciation string
	Example       string
	Conjugation   string
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
			INNER JOIN entries e ON e.id = c.entry_id
			WHERE c.deck_id = ?
			  AND d.telegram_user_id = ?
			  AND e.front_norm = lower(trim(?))
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
	entryID, err := s.resolveOrCreateEntry(ctx, params.DeckID, params.Front, params.Back, params.Pronunciation, params.Example, params.Conjugation)
	if err != nil {
		return domain.Card{}, err
	}

	result, err := s.db.ExecContext(
		ctx,
		`INSERT INTO cards (deck_id, entry_id, status) VALUES (?, ?, 'active')`,
		params.DeckID,
		entryID,
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
	query := `SELECT c.id, c.deck_id, c.entry_id, e.front, e.back, e.pronunciation, e.example, e.conjugation, c.status, c.next_due_at, c.interval_sec, c.ease, c.lapses, c.last_reviewed_at
		FROM cards c
		INNER JOIN entries e ON e.id = c.entry_id
		WHERE c.deck_id = ?`
	args := []any{deckID}
	if status != nil {
		query += ` AND c.status = ?`
		args = append(args, string(*status))
	}
	query += ` ORDER BY c.id ASC`

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
	query := `SELECT c.id, c.deck_id, c.entry_id, e.front, e.back, e.pronunciation, e.example, e.conjugation, c.status, c.next_due_at, c.interval_sec, c.ease, c.lapses, c.last_reviewed_at
		FROM cards c
		INNER JOIN decks d ON d.id = c.deck_id
		INNER JOIN entries e ON e.id = c.entry_id
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
		`SELECT c.id, c.deck_id, c.entry_id, e.front, e.back, e.pronunciation, e.example, e.conjugation, c.status, c.next_due_at, c.interval_sec, c.ease, c.lapses, c.last_reviewed_at
		 FROM cards c
		 INNER JOIN entries e ON e.id = c.entry_id
		 WHERE c.id = ?`,
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
		`SELECT c.id, c.deck_id, c.entry_id, e.front, e.back, e.pronunciation, e.example, e.conjugation, c.status, c.next_due_at, c.interval_sec, c.ease, c.lapses, c.last_reviewed_at
		 FROM cards c
		 INNER JOIN decks d ON d.id = c.deck_id
		 INNER JOIN entries e ON e.id = c.entry_id
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
		`SELECT c.id, c.deck_id, c.entry_id, e.front, e.back, e.pronunciation, e.example, e.conjugation, c.status, c.next_due_at, c.interval_sec, c.ease, c.lapses, c.last_reviewed_at
		 FROM cards c
		 INNER JOIN entries e ON e.id = c.entry_id
		 WHERE c.deck_id = ?
		   AND c.status = 'active'
		   AND c.next_due_at <= ?
		 ORDER BY c.next_due_at ASC, c.id ASC
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
		`SELECT c.id, c.deck_id, c.entry_id, e.front, e.back, e.pronunciation, e.example, e.conjugation, c.status, c.next_due_at, c.interval_sec, c.ease, c.lapses, c.last_reviewed_at
		 FROM cards c
		 INNER JOIN decks d ON d.id = c.deck_id
		 INNER JOIN entries e ON e.id = c.entry_id
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
		&card.EntryID,
		&card.Front,
		&card.Back,
		&card.Pronunciation,
		&card.Example,
		&card.Conjugation,
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

func (s *Store) resolveOrCreateEntry(ctx context.Context, deckID int64, front, back, pronunciation, example, conjugation string) (int64, error) {
	frontNorm := strings.ToLower(strings.TrimSpace(front))
	if frontNorm == "" {
		return 0, fmt.Errorf("front must not be empty")
	}
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO entries (
			language_from, language_to, front_norm, front, back, pronunciation, example, conjugation, updated_at
		)
		SELECT
			d.language_from, d.language_to, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP
		FROM decks d
		WHERE d.id = ?
		ON CONFLICT(language_from, language_to, front_norm) DO UPDATE SET
			front = excluded.front,
			back = excluded.back,
			pronunciation = excluded.pronunciation,
			example = excluded.example,
			conjugation = excluded.conjugation,
			updated_at = CURRENT_TIMESTAMP`,
		frontNorm,
		front,
		back,
		pronunciation,
		example,
		conjugation,
		deckID,
	); err != nil {
		return 0, fmt.Errorf("upsert entry: %w", err)
	}

	var entryID int64
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT e.id
		 FROM decks d
		 INNER JOIN entries e ON e.language_from = d.language_from
		 	AND e.language_to = d.language_to
		 	AND e.front_norm = ?
		 WHERE d.id = ?
		 LIMIT 1`,
		frontNorm,
		deckID,
	).Scan(&entryID); err != nil {
		return 0, fmt.Errorf("resolve entry id: %w", err)
	}
	return entryID, nil
}
