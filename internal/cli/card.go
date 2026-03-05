package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"word-learning/internal/ai"
	"word-learning/internal/app"
	"word-learning/internal/domain"
	"word-learning/internal/storage/sqlite"

	"github.com/spf13/cobra"
)

// runCardAdd adds a card to the active deck for user 0. Writes one line to out.
func runCardAdd(ctx context.Context, store *sqlite.Store, front, back, pronunciation, example, conjugation string, out io.Writer) (domain.Card, error) {
	service := app.NewService(store)
	card, err := service.AddCardForActiveDeckForUser(ctx, 0, front, back, pronunciation, example, conjugation)
	if err != nil {
		if errors.Is(err, app.ErrActiveDeckNotSet) {
			return domain.Card{}, fmt.Errorf("active deck is not set; run 'deck use <name...>'")
		}
		return domain.Card{}, err
	}
	if out != nil {
		_, _ = fmt.Fprintf(out, "Card created: id=%d deck=%d\n", card.ID, card.DeckID)
	}
	return card, nil
}

// runCardList lists cards for the active deck. Writes to out.
func runCardList(ctx context.Context, store *sqlite.Store, status string, out io.Writer) ([]domain.Card, error) {
	service := app.NewService(store)
	cards, err := service.ListCardsForActiveDeckForUser(ctx, 0, status)
	if err != nil {
		if errors.Is(err, app.ErrActiveDeckNotSet) {
			return nil, fmt.Errorf("active deck is not set; run 'deck use <name...>'")
		}
		return nil, err
	}
	if out != nil {
		printCardsTo(out, cards)
	}
	return cards, nil
}

// runCardGet returns the next card for the active deck and optional stats. Writes to out.
func runCardGet(ctx context.Context, store *sqlite.Store, out io.Writer) (*domain.Card, *app.DeckStats, error) {
	service := app.NewService(store)
	card, stats, err := service.NextCardWithStatsForActiveDeckForUser(ctx, 0)
	if err != nil {
		if errors.Is(err, app.ErrActiveDeckNotSet) {
			return nil, nil, fmt.Errorf("active deck is not set; run 'deck use <name...>'")
		}
		return nil, nil, err
	}
	if card == nil {
		if out != nil {
			_, _ = fmt.Fprintln(out, "No available cards right now.")
		}
		return nil, nil, nil
	}
	if out != nil {
		printCardDetailsTo(out, *card)
		_, _ = fmt.Fprintf(out, "Active %d, postponed %d, total %d\n", stats.Active, stats.Postponed, stats.Total)
	}
	return card, &stats, nil
}

// runCardRemember marks the card as remembered (longer interval).
func runCardRemember(ctx context.Context, store *sqlite.Store, cardID int64, out io.Writer) error {
	service := app.NewService(store)
	if err := service.RememberCardByID(ctx, cardID); err != nil {
		if errors.Is(err, app.ErrCardNotFound) {
			return fmt.Errorf("card %d not found", cardID)
		}
		return err
	}
	if out != nil {
		_, _ = fmt.Fprintf(out, "Card scheduled with longer interval: id=%d\n", cardID)
	}
	return nil
}

// runCardDontRemember schedules the card for short retry.
func runCardDontRemember(ctx context.Context, store *sqlite.Store, cardID int64, out io.Writer) error {
	service := app.NewService(store)
	if err := service.DontRememberCardByID(ctx, cardID); err != nil {
		if errors.Is(err, app.ErrCardNotFound) {
			return fmt.Errorf("card %d not found", cardID)
		}
		return err
	}
	if out != nil {
		_, _ = fmt.Fprintf(out, "Card scheduled for short retry: id=%d\n", cardID)
	}
	return nil
}

// runCardRemove soft-removes a card.
func runCardRemove(ctx context.Context, store *sqlite.Store, cardID int64, out io.Writer) error {
	service := app.NewService(store)
	if err := service.RemoveCardByID(ctx, cardID); err != nil {
		if errors.Is(err, app.ErrCardNotFound) {
			return fmt.Errorf("card %d not found", cardID)
		}
		return err
	}
	if out != nil {
		_, _ = fmt.Fprintf(out, "Card removed: id=%d\n", cardID)
	}
	return nil
}

// runCardRestore restores a removed card.
func runCardRestore(ctx context.Context, store *sqlite.Store, cardID int64, out io.Writer) error {
	service := app.NewService(store)
	if err := service.RestoreCardByID(ctx, cardID); err != nil {
		if errors.Is(err, app.ErrCardNotFound) {
			return fmt.Errorf("card %d not found", cardID)
		}
		return err
	}
	if out != nil {
		_, _ = fmt.Fprintf(out, "Card restored: id=%d\n", cardID)
	}
	return nil
}

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

func readBatchInput(fromFile string, fromStdin bool) ([]byte, error) {
	if (fromFile == "" && !fromStdin) || (fromFile != "" && fromStdin) {
		return nil, fmt.Errorf("exactly one input source is required: --from-file or --stdin")
	}
	if fromFile != "" {
		data, err := os.ReadFile(fromFile)
		if err != nil {
			return nil, fmt.Errorf("read --from-file: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile("/dev/stdin")
	if err != nil {
		return nil, fmt.Errorf("read --stdin: %w", err)
	}
	return data, nil
}

func writeBatchReport(out io.Writer, report app.BatchAddReport) {
	if out == nil {
		return
	}
	for _, item := range report.Items {
		reasonSuffix := ""
		if strings.TrimSpace(item.Reason) != "" {
			reasonSuffix = " (" + item.Reason + ")"
		}
		_, _ = fmt.Fprintf(out, "- %s => %s%s\n", item.FrontNormalized, item.Status, reasonSuffix)
	}
	_, _ = fmt.Fprintf(out, "Summary: total=%d created=%d skipped_duplicates=%d failed=%d\n",
		report.Summary.Total,
		report.Summary.Created,
		report.Summary.SkippedDuplicates,
		report.Summary.Failed,
	)
}

func runCardAddBatchAI(ctx context.Context, store *sqlite.Store, fromFile string, fromStdin bool, dryRun bool, out io.Writer) error {
	data, err := readBatchInput(fromFile, fromStdin)
	if err != nil {
		return err
	}
	generator, err := ai.NewGeneratorFromEnv()
	if err != nil {
		return err
	}
	service := app.NewService(store)
	report, err := service.AddCardsBatchAIForActiveDeckForUser(ctx, 0, generator, strings.Split(string(data), "\n"), app.BatchModeCLI, dryRun)
	if err != nil {
		if errors.Is(err, app.ErrActiveDeckNotSet) {
			return fmt.Errorf("active deck is not set; run 'deck use <name...>'")
		}
		return err
	}
	writeBatchReport(out, report)
	return nil
}

func newCardAddBatchAICmd(ctx *commandContext) *cobra.Command {
	var fromFile string
	var fromStdin bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "add-batch-ai",
		Short: "Add multiple cards with AI-generated fields",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCardAddBatchAI(cmd.Context(), ctx.Store, fromFile, fromStdin, dryRun, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&fromFile, "from-file", "", "Path to file with one front per line")
	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read fronts from stdin")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Generate and validate only, do not write to DB")
	return cmd
}

func newCardAddCmd(ctx *commandContext) *cobra.Command {
	var front string
	var back string
	var pronunciation string
	var example string
	var conjugation string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a card",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := runCardAdd(cmd.Context(), ctx.Store, front, back, pronunciation, example, conjugation, cmd.OutOrStdout())
			return err
		},
	}

	cmd.Flags().StringVar(&front, "front", "", "Front side (word/phrase)")
	cmd.Flags().StringVar(&back, "back", "", "Back side (translation)")
	cmd.Flags().StringVar(&pronunciation, "pronunciation", "", "Optional pronunciation (e.g. /banished/)")
	cmd.Flags().StringVar(&example, "example", "", "Optional usage example")
	cmd.Flags().StringVar(&conjugation, "conjugation", "", "Optional conjugation forms")
	_ = cmd.MarkFlagRequired("front")
	_ = cmd.MarkFlagRequired("back")

	return cmd
}

func newCardListCmd(ctx *commandContext) *cobra.Command {
	var status string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List cards",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := runCardList(cmd.Context(), ctx.Store, status, cmd.OutOrStdout())
			return err
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "Optional filter: active|removed")

	return cmd
}

func newCardRemoveCmd(ctx *commandContext) *cobra.Command {
	var cardID int64

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Soft-remove a card from active rotation",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCardRemove(cmd.Context(), ctx.Store, cardID, cmd.OutOrStdout())
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
			return runCardRestore(cmd.Context(), ctx.Store, cardID, cmd.OutOrStdout())
		},
	}

	cmd.Flags().Int64Var(&cardID, "id", 0, "Card ID")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newCardGetCmd(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get next available card for active deck",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, err := runCardGet(cmd.Context(), ctx.Store, cmd.OutOrStdout())
			return err
		},
	}
	return cmd
}

func newCardRememberCmd(ctx *commandContext) *cobra.Command {
	var cardID int64

	cmd := &cobra.Command{
		Use:   "remember",
		Short: "Increase next review interval",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCardRemember(cmd.Context(), ctx.Store, cardID, cmd.OutOrStdout())
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
			return runCardDontRemember(cmd.Context(), ctx.Store, cardID, cmd.OutOrStdout())
		},
	}

	cmd.Flags().Int64Var(&cardID, "id", 0, "Card ID")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
