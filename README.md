# word-learning-cli

CLI and Telegram bot for learning words with flashcards. One SQLite DB for both.

**Before anything:** Go 1.22+. Set `WORDLEARN_DB_PATH` to a file path (e.g. `./data/wordlearn.db`). Re-export in each new terminal or add to your shell profile.

**Pick a path:** CLI only (Scenario 1) · Bot (2) · Batch AI via CLI (3) · Bot in Docker (4)

---

## Setup (once per machine)

```bash
go mod tidy
export WORDLEARN_DB_PATH=./data/wordlearn.db
```

---

## Scenario 1: CLI only

Create a deck → set it active → add cards → review.

```bash
# 1) Create deck and set it active
go run ./cmd/wordcli deck create EN RU "English Basics"
go run ./cmd/wordcli deck use "English Basics"

# 2) Add a card, then get next and rate it
go run ./cmd/wordcli card add --front "banished" --back "изгнанный" --pronunciation "/banished/" --example "He was banished."
go run ./cmd/wordcli card get
go run ./cmd/wordcli card remember --id 1
go run ./cmd/wordcli card dont-remember --id 1
```

**More commands:** `deck list` · `deck current` · `card list [--status active|removed]` · `card remove --id N` · `card restore --id N`

---

## Scenario 2: Telegram bot

Same DB as CLI; decks and cards are shared.

```bash
export TELEGRAM_BOT_TOKEN="<from BotFather>"
export WORDLEARN_DB_PATH=./data/wordlearn.db
go run ./cmd/wordbot
```

**Menu (under the input):** After `/start` or `/help` you get two buttons.

| Button | What happens |
|--------|----------------|
| **Switch deck** | List of your decks (inline); tap one → it becomes active and the next card is shown. |
| **Add batch AI** | Choose deck (inline); then send one message with one word, phrase, or context sentence per line (see **Phrase and context mode** below). Bot fills back/pronunciation/example via AI. |

**Slash commands**

| Command | Parameters | Description |
|---------|------------|-------------|
| `/deck_create` | `<from> <to> <name...>` | e.g. `EN RU basics` |
| `/deck_use` | `<name...>` | e.g. `basics` |
| `/deck_list` | — | Lists your decks as inline buttons; tap to switch. |
| `/next` | — | Shows next due card (back in spoiler); buttons: **Don't remember**, **Remember**, **Remove**. |
| `/card_add` | front, back, pronunciation, example, conjugation | One message: five fields in order, separated by &#124;. Last two (example, conjugation) optional. |

---

## Scenario 3: Batch add with AI (CLI)

One word, phrase, or context sentence per line; AI fills back, pronunciation, example. **Requires:** active deck (do Scenario 1 first), `OPENAI_API_KEY`, prompt files in `./prompts` (e.g. `prompt_en-ru.txt`).

```bash
export OPENAI_API_KEY="<key>"
export WORDLEARN_DB_PATH=./data/wordlearn.db
go run ./cmd/wordcli deck use "English Basics"
printf "banished\ncome up with\n" | go run ./cmd/wordcli card add-batch-ai --stdin
```

From file: `go run ./cmd/wordcli card add-batch-ai --from-file words.txt`. Use `--dry-run` to test without saving.

**Phrase and context mode (batch AI):** Each line is one "front" for the AI. It can be:

- **Word or phrase** — e.g. `come up with`, `banished`. AI returns translation, pronunciation (IPA), and a short example sentence.
- **Context sentence** — a full sentence with the target word/phrase in **ALL CAPS**. AI treats the ALL CAPS part as the card front, uses your sentence as the `example` field, and translates only the target. Use this when you want the card to show a specific usage.

Example file for batch AI:
```text
banished
come up with
She was BANISHED from the court.
```

Same logic applies to the bot’s **Add batch AI**: send one message with one word, phrase, or context sentence per line.

---

## Scenario 4: Bot in Docker

Put `TELEGRAM_BOT_TOKEN` (and optionally `OPENAI_API_KEY`) in `.env`, then:

```bash
docker compose up -d --build
```

- DB in container: `/data/wordlearn.db` (host dir `./data` is mounted).
- Backup runs at container start; backup files appear in `./data/backups` on the host.
- To use the same DB from the host CLI: `WORDLEARN_DB_PATH=./data/wordlearn.db go run ./cmd/wordcli ...`

---

## Dev

- `go test ./...` — tests
- `make lint` — linter
- `make help` · `go run ./cmd/wordcli --help` — command list. In the bot: `/help`.
