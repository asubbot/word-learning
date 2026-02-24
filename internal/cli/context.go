package cli

import "word-learning-cli/internal/storage/sqlite"

type commandContext struct {
	Store *sqlite.Store
}
