package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"word-learning-cli/internal/storage/sqlite"
)

func newRootCmd() *cobra.Command {
	ctx := &commandContext{}

	rootCmd := &cobra.Command{
		Use:   "wordcli",
		Short: "CLI tool to study foreign words",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
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

	rootCmd.PersistentFlags().StringVar(&ctx.DBPath, "db", defaultDBPath(), "Path to SQLite database file")
	rootCmd.AddCommand(newDeckCmd(ctx))
	rootCmd.AddCommand(newCardCmd(ctx))

	return rootCmd
}

func defaultDBPath() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "wordcli.db"
	}
	return fmt.Sprintf("%s/%s", cwd, "wordcli.db")
}

func Execute() error {
	return newRootCmd().Execute()
}
