package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"word-learning/internal/storage/sqlite"
)

const dbPathEnvVar = "WORDLEARN_DB_PATH"

func newRootCmd() *cobra.Command {
	ctx := &commandContext{}

	rootCmd := &cobra.Command{
		Use:   "wordcli",
		Short: "CLI tool to study foreign words",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := resolveDBPath()
			if err != nil {
				return err
			}

			store, err := sqlite.Open(dbPath)
			if err != nil {
				return err
			}
			if err := store.InitSchema(context.Background()); err != nil {
				_ = store.Close()
				return err
			}

			ctx.Store = store
			return nil
		},
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			if ctx.Store != nil {
				_ = ctx.Store.Close()
			}
		},
	}

	rootCmd.AddCommand(newDeckCmd(ctx))
	rootCmd.AddCommand(newCardCmd(ctx))

	return rootCmd
}

func resolveDBPath() (string, error) {
	if v := strings.TrimSpace(os.Getenv(dbPathEnvVar)); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("database path is required: set %s", dbPathEnvVar)
}

func Execute() error {
	return newRootCmd().Execute()
}
