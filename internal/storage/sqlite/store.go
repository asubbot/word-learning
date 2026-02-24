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

CREATE TABLE IF NOT EXISTS entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  language_from TEXT NOT NULL,
  language_to TEXT NOT NULL,
  front_norm TEXT NOT NULL,
  front TEXT NOT NULL,
  back TEXT NOT NULL,
  pronunciation TEXT NOT NULL DEFAULT '',
  example TEXT NOT NULL DEFAULT '',
  conjugation TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS cards (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  deck_id INTEGER NOT NULL,
  entry_id INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
  next_due_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  interval_sec INTEGER NOT NULL DEFAULT 0,
  ease REAL NOT NULL DEFAULT 2.5,
  lapses INTEGER NOT NULL DEFAULT 0,
  last_reviewed_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(deck_id) REFERENCES decks(id),
  FOREIGN KEY(entry_id) REFERENCES entries(id)
);

CREATE INDEX IF NOT EXISTS idx_cards_deck_id ON cards(deck_id);
CREATE INDEX IF NOT EXISTS idx_cards_status ON cards(status);
CREATE INDEX IF NOT EXISTS idx_cards_deck_status ON cards(deck_id, status);
CREATE INDEX IF NOT EXISTS idx_decks_owner_id ON decks(telegram_user_id, id);
CREATE INDEX IF NOT EXISTS idx_active_decks_deck_id ON active_decks(deck_id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_entries_pair_front_norm ON entries(language_from, language_to, front_norm);
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
		{name: "backfill entries", run: s.backfillCardEntriesPreferLatest},
		{name: "compact cards schema", run: s.compactCardsSchemaWithoutLegacyColumns},
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
		{name: "entry_id", definition: "INTEGER"},
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

func (s *Store) backfillCardEntriesPreferLatest(ctx context.Context) error {
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO entries (
			language_from, language_to, front_norm, front, back, pronunciation, example, conjugation, updated_at
		)
		SELECT
			r.language_from,
			r.language_to,
			r.front_norm,
			r.front,
			r.back,
			r.pronunciation,
			r.example,
			r.conjugation,
			CURRENT_TIMESTAMP
		FROM (
			SELECT
				d.language_from,
				d.language_to,
				lower(trim(c.front)) AS front_norm,
				c.front,
				c.back,
				c.pronunciation,
				c.example,
				c.conjugation,
				ROW_NUMBER() OVER (
					PARTITION BY d.language_from, d.language_to, lower(trim(c.front))
					ORDER BY c.id DESC
				) AS rn
			FROM cards c
			INNER JOIN decks d ON d.id = c.deck_id
			WHERE trim(c.front) <> ''
		) r
		WHERE r.rn = 1
		ON CONFLICT(language_from, language_to, front_norm) DO UPDATE SET
			front = excluded.front,
			back = excluded.back,
			pronunciation = excluded.pronunciation,
			example = excluded.example,
			conjugation = excluded.conjugation,
			updated_at = CURRENT_TIMESTAMP`,
	); err != nil {
		msg := strings.ToLower(err.Error())
		// New schema DBs may not have legacy front/back columns to backfill from.
		if strings.Contains(msg, "no such column: c.front") || strings.Contains(msg, "no such column: front") {
			return nil
		}
		return fmt.Errorf("backfill entries from cards: %w", err)
	}

	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE cards
		 SET entry_id = (
			SELECT e.id
			FROM decks d
			INNER JOIN entries e ON e.language_from = d.language_from
				AND e.language_to = d.language_to
				AND e.front_norm = lower(trim(cards.front))
			WHERE d.id = cards.deck_id
			LIMIT 1
		 )
		 WHERE entry_id IS NULL`,
	); err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "no such column: cards.front") || strings.Contains(msg, "no such column: front") {
			return nil
		}
		return fmt.Errorf("backfill cards.entry_id: %w", err)
	}

	var missing int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM cards WHERE entry_id IS NULL`).Scan(&missing); err != nil {
		return fmt.Errorf("count cards with missing entry_id: %w", err)
	}
	if missing > 0 {
		return fmt.Errorf("backfill cards.entry_id incomplete: %d rows missing entry_id", missing)
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
		{
			name: "idx_cards_entry_id",
			sql:  `CREATE INDEX IF NOT EXISTS idx_cards_entry_id ON cards(entry_id)`,
		},
		{
			name: "ux_entries_pair_front_norm",
			sql:  `CREATE UNIQUE INDEX IF NOT EXISTS ux_entries_pair_front_norm ON entries(language_from, language_to, front_norm)`,
		},
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt.sql); err != nil {
			return fmt.Errorf("create index %s: %w", stmt.name, err)
		}
	}
	return nil
}

func (s *Store) compactCardsSchemaWithoutLegacyColumns(ctx context.Context) error {
	cols, err := s.cardColumnSet(ctx)
	if err != nil {
		return err
	}
	_, hasFront := cols["front"]
	_, hasBack := cols["back"]
	_, hasPron := cols["pronunciation"]
	_, hasExample := cols["example"]
	_, hasConj := cols["conjugation"]
	if !hasFront && !hasBack && !hasPron && !hasExample && !hasConj {
		// Already compact schema.
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign keys for cards compact: %w", err)
	}
	defer func() {
		_, _ = s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
	}()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cards compact transaction: %w", err)
	}
	rollback := func(cause error) error {
		_ = tx.Rollback()
		return cause
	}

	statements := []string{
		`CREATE TABLE cards_new (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			deck_id INTEGER NOT NULL,
			entry_id INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
			next_due_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			interval_sec INTEGER NOT NULL DEFAULT 0,
			ease REAL NOT NULL DEFAULT 2.5,
			lapses INTEGER NOT NULL DEFAULT 0,
			last_reviewed_at DATETIME NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY(deck_id) REFERENCES decks(id),
			FOREIGN KEY(entry_id) REFERENCES entries(id)
		)`,
		`INSERT INTO cards_new (id, deck_id, entry_id, status, next_due_at, interval_sec, ease, lapses, last_reviewed_at, created_at, updated_at)
		 SELECT id, deck_id, entry_id, status, next_due_at, interval_sec, ease, lapses, last_reviewed_at, created_at, updated_at
		 FROM cards`,
		`DROP TABLE cards`,
		`ALTER TABLE cards_new RENAME TO cards`,
	}
	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return rollback(fmt.Errorf("compact cards schema: %w", err))
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cards compact transaction: %w", err)
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

func (s *Store) cardColumnSet(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(cards)`)
	if err != nil {
		return nil, fmt.Errorf("read cards table info: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	set := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("scan cards table info: %w", err)
		}
		set[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cards table info: %w", err)
	}
	return set, nil
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
