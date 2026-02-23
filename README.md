# word-learning-cli

A Go CLI tool for learning foreign words with flashcards.

## MVP Features

- Deck management: create and list.
- Card management: add, list, remove, restore.
- CLI practice mode:
  - `card get` - fetch the next available card.
  - `card remember` - increase interval before next review (due-date scheduler).
  - `card dont-remember` - schedule a short retry (10 minutes).
- Reliable local storage in SQLite.

## Requirements

- Go 1.22+ (validated in this project environment).

## Install Dependencies

```bash
go mod tidy
```

## Quick Start

### 1) Create a deck

```bash
go run ./cmd/wordcli deck create --name "English Basics" --from EN --to RU
```

### 2) Add a card

```bash
go run ./cmd/wordcli card add --deck 1 --front "banished" --back "изгнанный" --pronunciation "/banished/" --description "He was banished from the kingdom."
```

### 3) Get a card

```bash
go run ./cmd/wordcli card get --deck 1
```

### 4) Mark as remembered (increase interval)

```bash
go run ./cmd/wordcli card remember --id 1
```

### 5) Mark as not remembered (short retry)

```bash
go run ./cmd/wordcli card dont-remember --id 1
```

## Main Commands

### Deck

- `deck create --name --from --to`
- `deck list`

### Card

- `card add --deck --front --back [--pronunciation] [--description]`
- `card list --deck [--status active|removed]`
- `card get --deck`
- `card remember --id`
- `card dont-remember --id`
- `card remove --id`
- `card restore --id`

## CLI Command Reference

### Global flags

- `--db <path>` - path to the SQLite DB file (default: `wordcli.db` in current directory).
- `-h, --help` - show help.

### Deck

- `deck create --name <name> --from <lang> --to <lang>`
  - creates a new deck;
  - `--from` and `--to` accept language codes with 2-8 latin letters (e.g. `EN`, `RU`);
  - source and target languages must be different.
- `deck list`
  - prints all existing decks.

### Card

- `card add --deck <deck_id> --front "<text>" --back "<text>" [--pronunciation "<text>"] [--description "<text>"]`
  - adds a card to the selected deck;
  - `--pronunciation` optionally stores transcription/pronunciation help.
- `card list --deck <deck_id> [--status active|removed]`
  - prints deck cards;
  - with `--status`, filters cards by status.
- `card get --deck <deck_id>`
  - prints the next available card for review;
  - returns only due cards (`next_due_at <= now`);
  - excludes `removed` cards;
  - prints summary line after the card: `Active X, postponed Y, total Z`.
- `card remember --id <card_id>`
  - increases review interval and schedules the card into the future (`next_due_at`).
- `card dont-remember --id <card_id>`
  - reduces interval and schedules a short retry in 10 minutes.
- `card remove --id <card_id>`
  - soft-removes a card from active rotation (`status=removed`).
- `card restore --id <card_id>`
  - restores a card to `active` status.

## Database Usage

By default, the tool uses `wordcli.db` in the current directory. You can set a custom path:

```bash
go run ./cmd/wordcli --db ./data/mywords.db deck list
```

## Quality Checks

```bash
go test ./...
go vet ./...
```

## Run CLI

```bash
go run ./cmd/wordcli
```

Subcommand examples:

```bash
go run ./cmd/wordcli completion --help
go run ./cmd/wordcli deck list
go run ./cmd/wordcli card get --deck 1
```

## Run Telegram Bot

The project also includes a Telegram bot binary that reuses the same app/storage layers as CLI.

### Required environment variables

- `TELEGRAM_BOT_TOKEN` - Telegram bot token from BotFather.
- `WORDCLI_DB_PATH` - SQLite DB path (optional, default: `./wordcli.db`).
- `TELEGRAM_POLLING_TIMEOUT` - long-poll timeout in seconds (optional, default: `30`).

### Start bot

```bash
export TELEGRAM_BOT_TOKEN="<your_token>"
export WORDCLI_DB_PATH="./bot.db"
go run ./cmd/wordbot
```

### Bot commands

- `/start` - show help.
- `/help` - show help.
- `/health` - health check.
- `/deck_create <name> <from> <to>` - create deck.
- `/deck_list` - list your decks.
- `/card_add <deck_id> | <front> | <back> | <pronunciation> | <description>` - add card.
- `/next <deck_id>` - show next due card with inline actions.

### Inline actions in `/next`

- `Don't remember` - schedule short retry.
- `Remember` - increase interval.
- `Remove` - remove card from active rotation.

Back side is rendered in Telegram spoiler format.

### Deployment notes

- Run `wordbot` as a long-lived process (systemd, Docker, or any process manager).
- Keep `TELEGRAM_BOT_TOKEN` in environment or secrets manager.
- CLI and bot can share one DB file, but production setups should prefer a stable volume path and regular backups.

## Shell Completion (Cobra)

`wordcli` supports generating shell completion scripts via the built-in command:

```bash
go run ./cmd/wordcli completion --help
```

Enable in current shell session:

```bash
# bash
source <(go run ./cmd/wordcli completion bash)

# zsh
source <(go run ./cmd/wordcli completion zsh)

# fish
go run ./cmd/wordcli completion fish | source
```

After that, `Tab` completion should suggest commands and flags (`card`, `deck`, `--db`, etc.).

Persistent setup for `zsh`:

```bash
echo 'source <(go run ./cmd/wordcli completion zsh)' >> ~/.zshrc
source ~/.zshrc
```

Run the command above from the project root.

## Manual E2E Scenario

Minimal end-to-end flow for a clean DB:

```bash
# 1) Remove previous test DB (if any)
rm -f ./e2e.db

# 2) Create a deck
go run ./cmd/wordcli --db ./e2e.db deck create --name "English Basics" --from EN --to RU

# 3) Add a card
go run ./cmd/wordcli --db ./e2e.db card add --deck 1 --front "banished" --back "изгнанный" --pronunciation "/banished/" --description "He was banished from the kingdom."

# 4) Verify card is active
go run ./cmd/wordcli --db ./e2e.db card list --deck 1 --status active

# 5) Get next card
go run ./cmd/wordcli --db ./e2e.db card get --deck 1

# 6) Mark as remembered (moves to future due date)
go run ./cmd/wordcli --db ./e2e.db card remember --id 1

# 7) Verify card is temporarily unavailable (due date not reached)
go run ./cmd/wordcli --db ./e2e.db card get --deck 1

# 8) Mark as not remembered (short retry)
go run ./cmd/wordcli --db ./e2e.db card dont-remember --id 1

# 9) Immediately after dont-remember, card may still be unavailable (~10 minute delay)
go run ./cmd/wordcli --db ./e2e.db card get --deck 1

# 10) Remove card from rotation
go run ./cmd/wordcli --db ./e2e.db card remove --id 1

# 11) Verify removed list
go run ./cmd/wordcli --db ./e2e.db card list --deck 1 --status removed

# 12) Restore card
go run ./cmd/wordcli --db ./e2e.db card restore --id 1

# 13) Final active list check
go run ./cmd/wordcli --db ./e2e.db card list --deck 1 --status active
```

## Makefile Commands

```bash
make help
make fmt
make test
make vet
make lint
make coverage
make coverage-html
make check
```

`make lint` uses `golangci-lint`; if missing, the command prints an install link.
`make coverage` prints textual coverage summary; `make coverage-html` generates `coverage.html`.
`make check` runs `fmt + vet + lint + coverage`.

## Bot E2E Smoke Scenario

Use one Telegram user and execute:

1. `/deck_create basics EN RU`
2. `/deck_list`
3. `/card_add 1 | banished | изгнанный | /banished/ | He was banished from the kingdom.`
4. `/next 1`
5. Press `Remember`
6. `/next 1` (expect no card due right away)
7. Press `Don't remember` on the next due card
8. Press `Remove`
9. `/next 1` (expect no active card until restore/new card)

## Telegram Verification Checklist

Use this checklist to validate end-to-end bot behavior in a live Telegram chat.

### 1) Start bot process

```bash
go run ./cmd/wordbot
```

Keep the process running during verification.

### 2) Basic command health

- `/start` -> bot returns help text.
- `/health` -> bot returns `OK`.
- `/deck_list` -> initially empty or existing user decks only.

### 3) Deck and card creation

1. `/deck_create basics EN RU`
2. `/deck_list` (verify deck appears)
3. `/card_add 1 | banished | изгнанный | /banished/ | He was banished from the kingdom.`

Expected: `Card created: id=<id> deck=1`.

### 4) Next card rendering

Run:

```text
/next 1
```

Expected:
- card details are rendered,
- back side is hidden using Telegram spoiler,
- stats line is present: `Active X, postponed Y, total Z`,
- inline actions are visible: `Don't remember`, `Remember`, `Remove`.

### 5) Inline action behavior

1. Press `Remember`
   - run `/next 1`, expect no due card immediately.
2. On the next due card, press `Don't remember`
   - card is rescheduled to short retry interval.
3. Press `Remove`
   - `/next 1` should not return removed card.

### 6) Negative validation checks

- `/next abc` -> validation error for non-numeric deck id.
- `/deck_create basics EN EN` -> validation error for same language pair.
- `/card_add 1 | only_front` -> usage/argument format error.

### 7) Cross-user isolation

From another Telegram account:

- `/deck_list` should not show first user decks.
- `/next 1` should not access first user cards.
- callback actions from another user's card message must be rejected.

### 8) Restart persistence

1. Stop bot process.
2. Start bot again (`go run ./cmd/wordbot`).
3. Verify `/deck_list` and `/next 1` still use persisted data from SQLite.

## Project Structure

- `cmd/wordcli` - CLI entrypoint.
- `cmd/wordbot` - Telegram bot entrypoint.
- `internal/cli` - Cobra commands and output formatting.
- `internal/bot` - Telegram routing, commands, and callback handling.
- `internal/app` - business logic and validation.
- `internal/storage/sqlite` - SQLite storage and queries.
- `internal/domain` - domain models.
- `migrations` - SQL schema/migrations.
