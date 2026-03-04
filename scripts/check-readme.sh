#!/usr/bin/env bash
# Verifies the CLI steps from README (Scenario 1 and related commands).
# Run from project root (e.g. make check-readme or ./scripts/check-readme.sh).

set -e

QUIET=
[[ "${1:-}" = "--quiet" ]] && QUIET=1

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

DB_PATH="${WORDLEARN_DB_PATH:-$ROOT/data/readme-check.db}"
mkdir -p "$(dirname "$DB_PATH")"
rm -f "$DB_PATH"

export WORDLEARN_DB_PATH="$DB_PATH"

run() {
  if [[ -n "$QUIET" ]]; then
    go run ./cmd/wordcli "$@" >/dev/null 2>&1
  else
    go run ./cmd/wordcli "$@"
  fi
}

[[ -z "$QUIET" ]] && echo "=== README check: Scenario 1 (CLI) ==="
run deck create EN RU "English Basics"
run deck use "English Basics"
run card add --front "banished" --back "изгнанный" --pronunciation "/banished/" --example "He was banished."
run card get
run card remember --id 1
run card dont-remember --id 1

[[ -z "$QUIET" ]] && echo "=== README check: more commands ==="
run deck list
run deck current
run card list --status active
run card list --status removed

# card remove / card restore (README "More commands")
run card remove --id 1
run card list --status removed
run card restore --id 1
run card list --status active

rm -f "$DB_PATH"
echo "README check OK"
