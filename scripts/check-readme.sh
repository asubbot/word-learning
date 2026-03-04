#!/usr/bin/env bash
# Verifies the CLI steps from README (Scenario 1 and related commands).
# Run from project root (e.g. make check-readme or ./scripts/check-readme.sh).

set -e

QUIET=
[[ "${1:-}" = "--quiet" ]] && QUIET=1

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# Always use a dedicated test DB; never use WORDLEARN_DB_PATH so we never touch the user's DB.
TEST_DB_PATH="$ROOT/data/readme-check.db"
if [[ -n "${WORDLEARN_DB_PATH:-}" ]]; then
  ENV_ABS="$(cd "$(dirname "$WORDLEARN_DB_PATH")" 2>/dev/null && pwd)/$(basename "$WORDLEARN_DB_PATH")"
  if [[ "$ENV_ABS" = "$TEST_DB_PATH" ]]; then
    echo "Error: WORDLEARN_DB_PATH must not point to the test DB (data/readme-check.db). Unset it or use another path." >&2
    exit 1
  fi
fi

mkdir -p "$(dirname "$TEST_DB_PATH")"
rm -f "$TEST_DB_PATH"

export WORDLEARN_DB_PATH="$TEST_DB_PATH"

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

rm -f "$TEST_DB_PATH"
echo "README check OK"
