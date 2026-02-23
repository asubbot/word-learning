# Due-date Scheduler Implementation Plan

## Goal
Switch next-card selection from FIFO (`ORDER BY id`) to a due-date approach: show cards whose review time has arrived (`next_due_at <= now`).

## Short answer about `snooze`
- **Do not remove `snooze` immediately** - use a safe 2-stage migration.
- **Recommendation:**
  1. Stage 1: keep `status=snoozed` for backward compatibility, but introduce due-date fields and logic.
  2. Stage 2: gradually remove `snoozed` and represent postponement only via `next_due_at` with `status=active`.

This reduces regression risk and avoids breaking existing commands.

## Stage 1. Introduce due-date without removing snooze

### 1) Extend DB schema
Add to `cards`:
- `next_due_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP`
- `interval_sec INTEGER NOT NULL DEFAULT 0`
- `ease REAL NOT NULL DEFAULT 2.5`
- `lapses INTEGER NOT NULL DEFAULT 0`
- `last_reviewed_at DATETIME NULL`

Index:
- `CREATE INDEX IF NOT EXISTS idx_cards_deck_due ON cards(deck_id, status, next_due_at);`

Validation:
- New DB is created with new columns and index.
- Existing DB migrates without errors.

### 2) Migrate existing data
- In `InitSchema`, add `ALTER TABLE ... ADD COLUMN ...` for the new fields.
- Backfill old cards:
  - `next_due_at = COALESCE(created_at, CURRENT_TIMESTAMP)`
  - `interval_sec=0`, `ease=2.5`, `lapses=0` (if empty)

Validation:
- Old cards become eligible for selection.
- No data loss.

### 3) Update next-card selection
New order:
- only cards from the selected deck,
- exclude `removed`,
- show only cards that are due:
  - `status='active' AND next_due_at <= now`
  - (temporary compatibility) `status='snoozed' AND snoozed_until <= now`
- ordering: `ORDER BY next_due_at ASC, id ASC`.

Validation:
- Overdue cards are selected first.
- Not-due cards are not selected.

### 3.1) Strategy for selecting overdue cards
Base due-date strategy:
- `ORDER BY next_due_at ASC, id ASC` (strict FIFO by due time).

Recommended UX compromise:
- **random-in-top-N**:
  1. SQL selects `N` most overdue cards (`ORDER BY next_due_at ASC LIMIT N`),
  2. app randomly picks one card from that set.

Why:
- preserves overdue priority,
- reduces monotony,
- avoids starvation risk of full random.

Recommended start:
- default mode: `strict-fifo`,
- optional mode: `random-top-n` with `N=20`.

Validation:
- `strict-fifo` is deterministic,
- `random-top-n` always picks from top-N,
- old overdue cards do not starve.

### 4) Card update rules
#### remember
- `interval_sec` grows:
  - if 0 -> 86400 (1 day)
  - else `max(86400, round(interval_sec * ease))`
- `ease = min(2.8, ease + 0.05)`
- `next_due_at = now + interval_sec`
- `last_reviewed_at = now`
- `status=active`

#### dont-remember
- `lapses = lapses + 1`
- `ease = max(1.3, ease - 0.2)`
- `interval_sec = 600` (10 minutes)
- `next_due_at = now + 600`
- `last_reviewed_at = now`
- `status=active`

#### remove / restore
- `remove`: keep current behavior `status=removed`
- `restore`: `status=active`, `next_due_at=now`

Validation:
- After `remember`, card is hidden until due.
- After `dont-remember`, card returns quickly.

### 5) Stats line in `card get`
Calculate and print:
- `Active X` = `status='active' AND next_due_at <= now`
- `postponed Y` = `status='active' AND next_due_at > now` + (temporarily) `status='snoozed'`
- `total Z` = all deck cards except `removed` (or all cards if chosen and documented in README)

Validation:
- X/Y update as expected after user actions.

### 6) Tests
Add/update tests:
- storage: due-date filtering, intervals, X/Y/Z aggregates
- app: remember/dont-remember with checks for `next_due_at`, `interval_sec`, `ease`, `lapses`
- e2e scenario in README

Validation:
- `make check` passes.
- Coverage does not regress.

### 7) Documentation
Update:
- `README.md`: explain due-date and interval behavior by action.
- `SPECIFICATION.md`: new fields and scheduling rules.

Validation:
- Docs match actual behavior.

## Stage 2. Model simplification (remove snooze)

### 2.1) Prepare data migration
Goal: move all postponed cards to unified due-date model.

Actions:
- Update rows:
  - `status='snoozed' -> status='active'`
  - `next_due_at = CASE`
    - if `snoozed_until` is later than `next_due_at`, use `snoozed_until`,
    - otherwise keep `next_due_at`.
- Run migration in a transaction.

Validation:
- No `status='snoozed'` rows remain.
- No card becomes due earlier than previous `snoozed_until`.

### 2.2) Simplify domain model
Actions:
- Remove `CardStatusSnoozed` usage from business logic.
- Keep only two statuses in code: `active`, `removed`.
- Keep `snoozed_until` temporarily as legacy column only (unused by logic).

Validation:
- Build passes with no `snoozed` references in app layer and card selection.

### 2.3) Update next-card query
Actions:
- Simplify selection condition:
  - `status='active' AND next_due_at <= now`
  - exclude `status='removed'`.
- Remove SQL branch `OR status='snoozed' ...`.

Validation:
- Behavior stays unchanged for normal workflows.
- Performance is not worse (index `idx_cards_deck_due` remains used).

### 2.4) Simplify `card get` stats
Actions:
- Recalculate without `snoozed`:
  - `active` = `status='active' AND next_due_at <= now`
  - `postponed` = `status='active' AND next_due_at > now`
  - `total` = `status != 'removed'`.

Validation:
- `Active X, postponed Y, total Z` matches SQL expectations.

### 2.5) Align CLI and text
Actions:
- Update help/README/SPEC text:
  - remove `snooze` as a separate status,
  - keep explanation that postponement is represented only by `next_due_at`.
- Optionally keep `--status snoozed` as temporary deprecated alias with warning.

Validation:
- User docs do not contradict real behavior.

### 2.6) Update tests
Actions:
- Rewrite tests that expected `status='snoozed'`.
- Add migration regression test:
  - pre-state: card is `snoozed` with future `snoozed_until`,
  - post-state: card is `active` with correct `next_due_at`.

Validation:
- `make check` passes.
- Migration and post-migration due-date selection are covered.

### 2.7) Final cleanup (optional)
Actions:
- After stabilization, remove `snoozed_until` from schema and code (separate migration).
- Remove deprecated alias and backward compatibility.

Validation:
- No `snoozed`/`snoozed_until` references remain in codebase.
- All e2e scenarios pass on a clean DB.

### Pros
- Fewer states and logic branches.
- Single scheduling mechanism.
- Easier maintenance and testing.

### Risks
- Requires careful migration of old rows.
- Requires synchronized updates of tests and docs.

## Final recommendation
- **Yes, `snooze` should be removed, but not immediately.**
- First introduce due-date in compatibility mode (Stage 1) and validate stability.
- Then clean up the model and remove `snoozed` (Stage 2).
