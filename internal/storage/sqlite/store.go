package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS decks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  language_from TEXT NOT NULL,
  language_to TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS cards (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  deck_id INTEGER NOT NULL,
  front TEXT NOT NULL,
  back TEXT NOT NULL,
  pronunciation TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
  next_due_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  interval_sec INTEGER NOT NULL DEFAULT 0,
  ease REAL NOT NULL DEFAULT 2.5,
  lapses INTEGER NOT NULL DEFAULT 0,
  last_reviewed_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(deck_id) REFERENCES decks(id)
);

CREATE INDEX IF NOT EXISTS idx_cards_deck_id ON cards(deck_id);
CREATE INDEX IF NOT EXISTS idx_cards_status ON cards(status);
CREATE INDEX IF NOT EXISTS idx_cards_deck_status ON cards(deck_id, status);
`

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) InitSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("initialize schema: %w", err)
	}

	if err := s.addCardColumnIfMissing(ctx, "pronunciation", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addCardColumnIfMissing(ctx, "next_due_at", "DATETIME"); err != nil {
		return err
	}
	if err := s.addCardColumnIfMissing(ctx, "interval_sec", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addCardColumnIfMissing(ctx, "ease", "REAL NOT NULL DEFAULT 2.5"); err != nil {
		return err
	}
	if err := s.addCardColumnIfMissing(ctx, "lapses", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addCardColumnIfMissing(ctx, "last_reviewed_at", "DATETIME NULL"); err != nil {
		return err
	}

	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE cards
		 SET next_due_at = COALESCE(next_due_at, created_at, CURRENT_TIMESTAMP)
		 WHERE next_due_at IS NULL`,
	); err != nil {
		return fmt.Errorf("backfill cards.next_due_at: %w", err)
	}

	if _, err := s.db.ExecContext(
		ctx,
		`CREATE INDEX IF NOT EXISTS idx_cards_deck_due ON cards(deck_id, status, next_due_at)`,
	); err != nil {
		return fmt.Errorf("create index idx_cards_deck_due: %w", err)
	}

	return nil
}

func (s *Store) addCardColumnIfMissing(ctx context.Context, name, definition string) error {
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE cards ADD COLUMN %s %s", name, definition)); err != nil {
		// Existing databases already migrated should ignore duplicate column errors.
		if !isDuplicateColumnError(err) {
			return fmt.Errorf("migrate cards.%s: %w", name, err)
		}
	}
	return nil
}

func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate column name")
}

func (s *Store) DB() *sql.DB {
	return s.db
}
