package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"word-learning-cli/internal/ai"
	"word-learning-cli/internal/app"
)

func newCardCmd(ctx *commandContext) *cobra.Command {
	cardCmd := &cobra.Command{
		Use:   "card",
		Short: "Manage cards",
	}

	cardCmd.AddCommand(newCardAddCmd(ctx))
	cardCmd.AddCommand(newCardListCmd(ctx))
	cardCmd.AddCommand(newCardRemoveCmd(ctx))
	cardCmd.AddCommand(newCardRestoreCmd(ctx))
	cardCmd.AddCommand(newCardGetCmd(ctx))
	cardCmd.AddCommand(newCardRememberCmd(ctx))
	cardCmd.AddCommand(newCardDontRememberCmd(ctx))
	cardCmd.AddCommand(newCardAddBatchAICmd(ctx))

	return cardCmd
}

func newCardAddBatchAICmd(ctx *commandContext) *cobra.Command {
	var deckID int64
	var fromFile string
	var fromStdin bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "add-batch-ai",
		Short: "Add multiple cards with AI-generated fields",
		RunE: func(cmd *cobra.Command, args []string) error {
			if deckID <= 0 {
				return fmt.Errorf("--deck must be a positive integer")
			}
			if (fromFile == "" && !fromStdin) || (fromFile != "" && fromStdin) {
				return fmt.Errorf("exactly one input source is required: --from-file or --stdin")
			}

			var data []byte
			var err error
			if fromFile != "" {
				data, err = os.ReadFile(fromFile)
				if err != nil {
					return fmt.Errorf("read --from-file: %w", err)
				}
			} else {
				data, err = os.ReadFile("/dev/stdin")
				if err != nil {
					return fmt.Errorf("read --stdin: %w", err)
				}
			}

			generator, err := ai.NewGeneratorFromEnv()
			if err != nil {
				return err
			}
			service := app.NewService(ctx.Store)
			report, err := service.AddCardsBatchAIToDeck(context.Background(), generator, app.BatchAddAIParams{
				DeckID: deckID,
				Lines:  strings.Split(string(data), "\n"),
				Mode:   app.BatchModeCLI,
				DryRun: dryRun,
			})
			if err != nil {
				return err
			}

			for _, item := range report.Items {
				reasonSuffix := ""
				if strings.TrimSpace(item.Reason) != "" {
					reasonSuffix = " (" + item.Reason + ")"
				}
				fmt.Printf("- %s => %s%s\n", item.FrontNormalized, item.Status, reasonSuffix)
			}
			fmt.Printf("Summary: total=%d created=%d skipped_duplicates=%d failed=%d\n",
				report.Summary.Total,
				report.Summary.Created,
				report.Summary.SkippedDuplicates,
				report.Summary.Failed,
			)
			return nil
		},
	}

	cmd.Flags().Int64Var(&deckID, "deck", 0, "Deck ID")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to file with one front per line")
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read fronts from stdin")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Generate and validate only, do not write to DB")
	_ = cmd.MarkFlagRequired("deck")
	return cmd
}

func newCardAddCmd(ctx *commandContext) *cobra.Command {
	var deckID int64
	var front string
	var back string
	var pronunciation string
	var example string
	var conjugation string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a card",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			card, err := service.AddCardToDeck(context.Background(), deckID, front, back, pronunciation, example, conjugation)
			if err != nil {
				return err
			}
			fmt.Printf("Card created: id=%d deck=%d\n", card.ID, card.DeckID)
			return nil
		},
	}

	cmd.Flags().Int64Var(&deckID, "deck", 0, "Deck ID")
	cmd.Flags().StringVar(&front, "front", "", "Front side (word/phrase)")
	cmd.Flags().StringVar(&back, "back", "", "Back side (translation)")
	cmd.Flags().StringVar(&pronunciation, "pronunciation", "", "Optional pronunciation (e.g. /banished/)")
	cmd.Flags().StringVar(&example, "example", "", "Optional usage example")
	cmd.Flags().StringVar(&conjugation, "conjugation", "", "Optional conjugation forms")
	_ = cmd.MarkFlagRequired("deck")
	_ = cmd.MarkFlagRequired("front")
	_ = cmd.MarkFlagRequired("back")

	return cmd
}

func newCardListCmd(ctx *commandContext) *cobra.Command {
	var deckID int64
	var status string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List cards",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			cards, err := service.ListCardsInDeck(context.Background(), deckID, status)
			if err != nil {
				return err
			}
			printCards(cards)
			return nil
		},
	}

	cmd.Flags().Int64Var(&deckID, "deck", 0, "Deck ID")
	cmd.Flags().StringVar(&status, "status", "", "Optional filter: active|removed")
	_ = cmd.MarkFlagRequired("deck")

	return cmd
}

func newCardRemoveCmd(ctx *commandContext) *cobra.Command {
	var cardID int64

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Soft-remove a card from active rotation",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			if err := service.RemoveCardByID(context.Background(), cardID); err != nil {
				if errors.Is(err, app.ErrCardNotFound) {
					return fmt.Errorf("card %d not found", cardID)
				}
				return err
			}
			fmt.Printf("Card removed: id=%d\n", cardID)
			return nil
		},
	}

	cmd.Flags().Int64Var(&cardID, "id", 0, "Card ID")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newCardRestoreCmd(ctx *commandContext) *cobra.Command {
	var cardID int64

	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore a removed card to active status",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			if err := service.RestoreCardByID(context.Background(), cardID); err != nil {
				if errors.Is(err, app.ErrCardNotFound) {
					return fmt.Errorf("card %d not found", cardID)
				}
				return err
			}
			fmt.Printf("Card restored: id=%d\n", cardID)
			return nil
		},
	}

	cmd.Flags().Int64Var(&cardID, "id", 0, "Card ID")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newCardGetCmd(ctx *commandContext) *cobra.Command {
	var deckID int64

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get next available card for a deck",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			card, stats, err := service.NextCardWithStatsInDeck(context.Background(), deckID)
			if err != nil {
				return err
			}
			if card == nil {
				fmt.Println("No available cards right now.")
				return nil
			}
			printCardDetails(*card)
			fmt.Printf("Active %d, postponed %d, total %d\n", stats.Active, stats.Postponed, stats.Total)
			return nil
		},
	}

	cmd.Flags().Int64Var(&deckID, "deck", 0, "Deck ID")
	_ = cmd.MarkFlagRequired("deck")
	return cmd
}

func newCardRememberCmd(ctx *commandContext) *cobra.Command {
	var cardID int64

	cmd := &cobra.Command{
		Use:   "remember",
		Short: "Increase next review interval",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			if err := service.RememberCardByID(context.Background(), cardID); err != nil {
				if errors.Is(err, app.ErrCardNotFound) {
					return fmt.Errorf("card %d not found", cardID)
				}
				return err
			}
			fmt.Printf("Card scheduled with longer interval: id=%d\n", cardID)
			return nil
		},
	}

	cmd.Flags().Int64Var(&cardID, "id", 0, "Card ID")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newCardDontRememberCmd(ctx *commandContext) *cobra.Command {
	var cardID int64

	cmd := &cobra.Command{
		Use:   "dont-remember",
		Short: "Schedule short retry interval",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			if err := service.DontRememberCardByID(context.Background(), cardID); err != nil {
				if errors.Is(err, app.ErrCardNotFound) {
					return fmt.Errorf("card %d not found", cardID)
				}
				return err
			}
			fmt.Printf("Card scheduled for short retry: id=%d\n", cardID)
			return nil
		},
	}

	cmd.Flags().Int64Var(&cardID, "id", 0, "Card ID")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
