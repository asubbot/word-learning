-- Final cleanup after due-date migration.
-- Apply only after all environments no longer rely on snoozed legacy data.

DROP INDEX IF EXISTS idx_cards_snoozed_until;
ALTER TABLE cards DROP COLUMN snoozed_until;
