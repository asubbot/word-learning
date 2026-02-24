# Specification: CLI Tool for Learning Foreign Words

## 1. Goal
Build a CLI tool for spaced learning of words/phrases using flashcards (front/back).

## 2. Usage Context
- The user interacts with the tool via terminal.
- CLI is used as a service layer for:
  - managing cards in a deck,
  - fetching the next card,
  - managing decks.

## 3. Functional Requirements

### 3.1 Multi-language support
- The system must support learning cards across multiple languages.
- One deck should contain cards for one language pair (for example, `EN -> RU`), or an explicitly configured deck pair.
- A user may have multiple decks for different language pairs.

### 3.2 Card model
Each card must contain at least:
- `id`
- `front` - front side (word/phrase)
- `back` - back side (translation)
- `pronunciation` - pronunciation/transcription (optional)
- `description` - note/example
- `language_from`
- `language_to`
- `status` (`active`, `removed`)
- `next_due_at` (timestamp for next due review)
- `interval_sec` (current review interval)
- `ease` (interval growth coefficient)
- `lapses` (number of failed recalls)
- `last_reviewed_at` (time of last answer)
- `created_at`, `updated_at`

### 3.3 Batch card creation via AI
The system must support creating multiple cards from a list of fronts (one word/phrase per line), where AI fills missing fields:
- `back`
- `pronunciation`
- `description`

The language pair for generation must be taken from the selected deck (`language_from -> language_to`).

Input normalization rules:
- trim leading/trailing spaces for each line;
- ignore empty lines;
- ignore comment lines that start with `#` (for CLI file/stdin use).

Duplicate behavior:
- if a card with the same `front` already exists in the same deck (case-insensitive, trimmed comparison), it must be skipped with a clear reason in the report.

#### Variant A: Telegram bot batch flow
- User submits batch input in one message:
  - first line: command + deck id
  - next lines: one front per line.
- Example:
  - `/card_add_batch_ai 1`
  - `banished`
  - `crack down on sth`
  - `come up with`
- Bot validates input, calls AI for each line, saves valid cards, and returns a summary:
  - total lines
  - created
  - skipped duplicates
  - failed generations/validations

#### Variant B: CLI batch flow
- CLI command accepts batch input from file or stdin:
  - `wordcli card add-batch-ai --deck <id> --from-file <path>`
  - `cat words.txt | wordcli card add-batch-ai --deck <id> --stdin`
- CLI supports `--dry-run` mode:
  - generate and validate fields,
  - print preview and summary,
  - do not write to DB.
- On save mode, CLI writes valid cards and prints final summary:
  - total
  - created
  - skipped duplicates
  - failed

## 6. Non-functional requirements
- Simplicity (KISS): minimal architectural complexity for MVP.
- Storage reliability: card data must not be lost after restart.
- Batch AI operations must be observable: each batch returns deterministic summary counts.

## 7. Data model (minimum)

### Table `decks`
- `id` (PK)
- `name`
- `language_from`
- `language_to`
- `created_at`
- `updated_at`

### Table `cards`
- `id` (PK)
- `deck_id` (FK -> decks.id)
- `front`
- `back`
- `pronunciation`
- `description`
- `status` (`active`, `removed`)
- `next_due_at`
- `interval_sec`
- `ease`
- `lapses`
- `last_reviewed_at`
- `created_at`
- `updated_at`
