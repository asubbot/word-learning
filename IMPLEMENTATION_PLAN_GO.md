# Step-by-step Go CLI Implementation Plan (MVP)

## Step 1. Initialize project and CLI scaffold
- Initialize Go module.
- Add Cobra.
- Add `deck` and `card` commands.

Validation:
- `go mod tidy`
- `go run ./cmd/wordcli --help`
- `go run ./cmd/wordcli deck --help`
- `go run ./cmd/wordcli card --help`

## Step 2. Data storage and DB schema
- Add SQLite.
- Add schema initialization for `decks` and `cards` tables.
- Add `--db` flag.

Validation:
- DB file is created on first run.
- `decks` and `cards` tables exist.
- Re-run does not break schema.

## Step 3. Implement deck commands
- `deck create --name --from --to`
- `deck list`
- Validate required flags and language codes.

Validation:
- After `deck create`, deck appears in `deck list`.
- Missing-flag errors are clear.

## Step 4. Implement card commands (CRUD + statuses)
- `card add --deck --front --back --description`
- `card list --deck [--status]`
- `card remove --id`
- `card restore --id`

Validation:
- Card appears in `active`.
- After remove, card is visible in `removed`.
- After restore, card is `active` again.

## Step 5. Learning logic
- `card get --deck`
- `card remember --id` (24 hours)
- `card dont-remember --id`

Validation:
- After `remember`, card is not returned.
- Card appears again when due.
- `dont-remember` keeps card in rotation with a short retry interval.

## Step 6. CLI UX and messages
- Unified output format for `list`.
- Clear errors.

Validation:
- Output is stable and readable.
- Errors include reason and context.

## Step 7. Tests and quality
- Unit tests for business logic.
- Integration tests for storage layer.

Validation:
- `go test ./...`
- `go vet ./...`

## Step 8. Documentation
- README with quickstart and command examples.

Validation:
- A new user can run the project from README without extra steps.
