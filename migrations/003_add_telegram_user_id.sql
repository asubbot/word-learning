ALTER TABLE decks ADD COLUMN telegram_user_id INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_decks_owner_id ON decks(telegram_user_id, id);
