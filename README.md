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
go run ./cmd/wordcli card add --deck 1 --front "banished" --back "exiled" --pronunciation "/banished/" --description "He was banished from the kingdom."
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
go run ./cmd/wordcli --db ./e2e.db card add --deck 1 --front "banished" --back "exiled" --pronunciation "/banished/" --description "He was banished from the kingdom."

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

## Project Structure

- `cmd/wordcli` - CLI entrypoint.
- `internal/cli` - Cobra commands and output formatting.
- `internal/app` - business logic and validation.
- `internal/storage/sqlite` - SQLite storage and queries.
- `internal/domain` - domain models.
- `migrations` - SQL schema/migrations.
