#!/usr/bin/env bash
# Backs up the wordlearn SQLite DB to data/backups/ with a date suffix.
# Keeps the last KEEP_DAYS backups (default 7). Run from project root or via cron.
#
# Usage: ./scripts/backup-wordlearn-db.sh
# Env:   WORDLEARN_DB_PATH (default: ./data/wordlearn.db)
#        WORDLEARN_BACKUP_DIR (default: ./data/backups)
#        WORDLEARN_BACKUP_KEEP_DAYS (default: 7)

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SRC_DB="${WORDLEARN_DB_PATH:-$ROOT/data/wordlearn.db}"
BACKUP_DIR="${WORDLEARN_BACKUP_DIR:-$ROOT/data/backups}"
KEEP_DAYS="${WORDLEARN_BACKUP_KEEP_DAYS:-7}"

if [[ ! -f "$SRC_DB" ]]; then
  echo "Error: database not found at $SRC_DB" >&2
  exit 1
fi

mkdir -p "$BACKUP_DIR"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
BASENAME="$(basename "$SRC_DB" .db)"
DEST="$BACKUP_DIR/${BASENAME}-${TIMESTAMP}.db"

cp "$SRC_DB" "$DEST"
echo "Backed up to $DEST"

# Prune old backups: keep last KEEP_DAYS days (by file mtime)
find "$BACKUP_DIR" -maxdepth 1 -name "${BASENAME}-*.db" -type f -mtime +$KEEP_DAYS -delete
