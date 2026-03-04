package cli

import "word-learning/internal/storage/sqlite"

type commandContext struct {
	Store *sqlite.Store
}
