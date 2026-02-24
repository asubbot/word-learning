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
  telegram_user_id INTEGER NOT NULL DEFAULT 0,
  name TEXT NOT NULL,
  language_from TEXT NOT NULL,
  language_to TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS active_decks (
  user_id INTEGER PRIMARY KEY,
  deck_id INTEGER NOT NULL,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(deck_id) REFERENCES decks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS cards (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  deck_id INTEGER NOT NULL,
  front TEXT NOT NULL,
  back TEXT NOT NULL,
  pronunciation TEXT NOT NULL DEFAULT '',
  example TEXT NOT NULL DEFAULT '',
  conjugation TEXT NOT NULL DEFAULT '',
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
CREATE INDEX IF NOT EXISTS idx_decks_owner_id ON decks(telegram_user_id, id);
CREATE INDEX IF NOT EXISTS idx_active_decks_deck_id ON active_decks(deck_id);
`

type Store struct {
	db *sql.DB
}

type schemaStep struct {
	name string
	run  func(context.Context) error
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
	return s.runSchemaSteps(ctx, []schemaStep{
		{name: "initialize schema", run: s.applyBaseSchema},
		{name: "ensure legacy columns", run: s.ensureLegacyColumns},
		{name: "backfill examples", run: s.backfillCardExampleFromDescription},
		{name: "backfill due dates", run: s.backfillCardDueDates},
		{name: "ensure indexes", run: s.ensureIndexes},
	})
}

func (s *Store) runSchemaSteps(ctx context.Context, steps []schemaStep) error {
	for _, step := range steps {
		if err := step.run(ctx); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return nil
}

func (s *Store) applyBaseSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	return nil
}

func (s *Store) ensureLegacyColumns(ctx context.Context) error {
	cardColumns := []struct {
		name       string
		definition string
	}{
		{name: "pronunciation", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "example", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "conjugation", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "next_due_at", definition: "DATETIME"},
		{name: "interval_sec", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "ease", definition: "REAL NOT NULL DEFAULT 2.5"},
		{name: "lapses", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "last_reviewed_at", definition: "DATETIME NULL"},
	}
	for _, column := range cardColumns {
		if err := s.addCardColumnIfMissing(ctx, column.name, column.definition); err != nil {
			return err
		}
	}
	if err := s.addDeckColumnIfMissing(ctx, "telegram_user_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return nil
}

func (s *Store) backfillCardDueDates(ctx context.Context) error {
	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE cards
		 SET next_due_at = COALESCE(next_due_at, created_at, CURRENT_TIMESTAMP)
		 WHERE next_due_at IS NULL`,
	); err != nil {
		return fmt.Errorf("backfill cards.next_due_at: %w", err)
	}
	return nil
}

func (s *Store) backfillCardExampleFromDescription(ctx context.Context) error {
	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE cards
		 SET example = COALESCE(NULLIF(example, ''), description, '')
		 WHERE example = ''`,
	); err != nil {
		// Old databases may not have description column (new schema). Ignore missing-column errors.
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "no such column: description") {
			return nil
		}
		return fmt.Errorf("backfill cards.example from description: %w", err)
	}
	return nil
}

func (s *Store) ensureIndexes(ctx context.Context) error {
	statements := []struct {
		name string
		sql  string
	}{
		{
			name: "idx_cards_deck_due",
			sql:  `CREATE INDEX IF NOT EXISTS idx_cards_deck_due ON cards(deck_id, status, next_due_at)`,
		},
		{
			name: "idx_decks_owner_id",
			sql:  `CREATE INDEX IF NOT EXISTS idx_decks_owner_id ON decks(telegram_user_id, id)`,
		},
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt.sql); err != nil {
			return fmt.Errorf("create index %s: %w", stmt.name, err)
		}
	}
	return nil
}

func (s *Store) addDeckColumnIfMissing(ctx context.Context, name, definition string) error {
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE decks ADD COLUMN %s %s", name, definition)); err != nil {
		if !isDuplicateColumnError(err) {
			return fmt.Errorf("migrate decks.%s: %w", name, err)
		}
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
