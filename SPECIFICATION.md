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

## 6. Non-functional requirements
- Simplicity (KISS): minimal architectural complexity for MVP.
- Storage reliability: card data must not be lost after restart.

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
