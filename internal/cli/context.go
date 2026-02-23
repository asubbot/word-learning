package cli

import "word-learning-cli/internal/storage/sqlite"

type commandContext struct {
	DBPath string
	Store  *sqlite.Store
}
