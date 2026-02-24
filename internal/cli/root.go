package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"word-learning-cli/internal/storage/sqlite"
)

const dbPathEnvVar = "WORDCLI_DB_PATH"

func newRootCmd() *cobra.Command {
	ctx := &commandContext{}

	rootCmd := &cobra.Command{
		Use:   "wordcli",
		Short: "CLI tool to study foreign words",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := resolveDBPath(ctx.DBPath)
			if err != nil {
				return err
			}
			ctx.DBPath = dbPath

			store, err := sqlite.Open(ctx.DBPath)
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

	rootCmd.PersistentFlags().StringVar(&ctx.DBPath, "db", "", "Path to SQLite database file")
	rootCmd.AddCommand(newDeckCmd(ctx))
	rootCmd.AddCommand(newCardCmd(ctx))

	return rootCmd
}

func resolveDBPath(flagValue string) (string, error) {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(os.Getenv(dbPathEnvVar)); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("database path is required: pass --db or set %s", dbPathEnvVar)
}

func Execute() error {
	return newRootCmd().Execute()
}
