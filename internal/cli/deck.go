package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"word-learning/internal/app"
	"word-learning/internal/domain"
	"word-learning/internal/export"
	"word-learning/internal/storage/sqlite"

	"github.com/spf13/cobra"
)

// runDeckCreate creates a deck and writes a line to out. Returns the created deck or error.
func runDeckCreate(ctx context.Context, store *sqlite.Store, languageFrom, languageTo, name string, out io.Writer) (domain.Deck, error) {
	service := app.NewService(store)
	deck, err := service.CreateDeck(ctx, name, languageFrom, languageTo)
	if err != nil {
		return domain.Deck{}, err
	}
	if out != nil {
		_, _ = fmt.Fprintf(out, "Deck created: id=%d name=%q pair=%s->%s\n", deck.ID, deck.Name, deck.LanguageFrom, deck.LanguageTo)
	}
	return deck, nil
}

// runDeckList lists all decks and writes to out.
func runDeckList(ctx context.Context, store *sqlite.Store, out io.Writer) ([]domain.Deck, error) {
	service := app.NewService(store)
	decks, err := service.ListDecksAll(ctx)
	if err != nil {
		return nil, err
	}
	if out != nil {
		printDecksAllTo(out, decks)
	}
	return decks, nil
}

// runDeckUse sets the active deck by name for user 0. Returns the deck or error.
func runDeckUse(ctx context.Context, store *sqlite.Store, name string, out io.Writer) (*domain.Deck, error) {
	service := app.NewService(store)
	result, err := service.DeckUseForUser(ctx, 0, name)
	if err != nil {
		if errors.Is(err, app.ErrDeckNameAmbiguous) && out != nil {
			_, _ = fmt.Fprintln(out, "Deck name is ambiguous. Candidates:")
			for _, d := range result.Candidates {
				_, _ = fmt.Fprintf(out, "- %s (%s->%s)\n", d.Name, d.LanguageFrom, d.LanguageTo)
			}
			return nil, fmt.Errorf("please retry with exact deck name")
		}
		return nil, err
	}
	if result.Deck == nil {
		return nil, fmt.Errorf("failed to set active deck")
	}
	if out != nil {
		_, _ = fmt.Fprintf(out, "Active deck: %s (%s->%s)\n", result.Deck.Name, result.Deck.LanguageFrom, result.Deck.LanguageTo)
	}
	return result.Deck, nil
}

// runDeckCurrent returns the current active deck for user 0.
func runDeckCurrent(ctx context.Context, store *sqlite.Store, out io.Writer) (*domain.Deck, error) {
	service := app.NewService(store)
	deck, err := service.DeckCurrentForUser(ctx, 0)
	if err != nil {
		return nil, err
	}
	if deck == nil {
		return nil, fmt.Errorf("active deck is not set; run 'deck use <name...>'")
	}
	if out != nil {
		_, _ = fmt.Fprintf(out, "Active deck: %s (%s->%s)\n", deck.Name, deck.LanguageFrom, deck.LanguageTo)
	}
	return deck, nil
}

func resolveDeckForExport(ctx context.Context, store *sqlite.Store, deckName string, out io.Writer) (*domain.Deck, error) {
	if deckName != "" {
		deck, err := store.FindDeckByExactNameForOwner(ctx, 0, deckName)
		if err != nil {
			return nil, err
		}
		if deck == nil {
			candidates, _ := store.FindDeckCandidatesForOwner(ctx, 0, deckName, 10)
			if len(candidates) > 0 && out != nil {
				_, _ = fmt.Fprintln(out, "Deck name is ambiguous. Candidates:")
				for _, d := range candidates {
					_, _ = fmt.Fprintf(out, "- %s (%s->%s)\n", d.Name, d.LanguageFrom, d.LanguageTo)
				}
				return nil, fmt.Errorf("please retry with exact deck name")
			}
			return nil, fmt.Errorf("deck %q not found", deckName)
		}
		return deck, nil
	}
	deck, err := store.GetActiveDeckForUser(ctx, 0)
	if err != nil {
		return nil, err
	}
	if deck == nil {
		return nil, fmt.Errorf("active deck is not set; use --deck <name> or run 'deck use <name...>'")
	}
	return deck, nil
}

func runDeckExport(ctx context.Context, store *sqlite.Store, deckName, outputPath string, out io.Writer) ([]byte, error) {
	deck, err := resolveDeckForExport(ctx, store, deckName, out)
	if err != nil {
		return nil, err
	}
	service := app.NewService(store)
	data, err := service.ExportDeckForUser(ctx, deck.TelegramUserID, deck.ID)
	if err != nil {
		return nil, err
	}
	if outputPath != "" {
		if err := os.WriteFile(outputPath, data, 0o644); err != nil {
			return nil, fmt.Errorf("write file: %w", err)
		}
		if out != nil {
			_, _ = fmt.Fprintf(out, "Exported deck %q to %s\n", deck.Name, outputPath)
		}
	} else if out != nil {
		_, _ = out.Write(data)
	}
	return data, nil
}

func findSuitableDeckForImport(decks []domain.Deck, normalizedFrom, normalizedTo string) (*domain.Deck, error) {
	var suitable []domain.Deck
	for _, d := range decks {
		if d.LanguageFrom == normalizedFrom && d.LanguageTo == normalizedTo {
			suitable = append(suitable, d)
		}
	}
	switch len(suitable) {
	case 1:
		return &suitable[0], nil
	case 0:
		return nil, fmt.Errorf("no deck with %s->%s: use --new <name> to create one", normalizedFrom, normalizedTo)
	default:
		return nil, fmt.Errorf("multiple decks with %s->%s: use --deck <name> or --new <name>", normalizedFrom, normalizedTo)
	}
}

func resolveTargetDeckForImport(ctx context.Context, store *sqlite.Store, svc *app.Service, exp *export.DeckExport, deckName, newDeckName string) (*domain.Deck, error) {
	normalizedFrom := strings.ToUpper(strings.TrimSpace(exp.Deck.LanguageFrom))
	normalizedTo := strings.ToUpper(strings.TrimSpace(exp.Deck.LanguageTo))

	if deckName != "" {
		deck, err := store.FindDeckByExactNameForOwner(ctx, 0, deckName)
		if err != nil {
			return nil, err
		}
		if deck == nil {
			return nil, fmt.Errorf("deck %q not found", deckName)
		}
		if deck.LanguageFrom != normalizedFrom || deck.LanguageTo != normalizedTo {
			return nil, fmt.Errorf("deck %q has %s->%s, import is %s->%s", deckName, deck.LanguageFrom, deck.LanguageTo, normalizedFrom, normalizedTo)
		}
		return deck, nil
	}
	if newDeckName != "" {
		deck, err := svc.CreateDeckForUser(ctx, 0, newDeckName, exp.Deck.LanguageFrom, exp.Deck.LanguageTo)
		if err != nil {
			return nil, err
		}
		return &deck, nil
	}

	decks, err := svc.ListDecksForUser(ctx, 0)
	if err != nil {
		return nil, err
	}
	return findSuitableDeckForImport(decks, normalizedFrom, normalizedTo)
}

// runDeckImport imports cards from a JSON file into an existing or new deck.
func runDeckImport(ctx context.Context, store *sqlite.Store, filePath string, deckName, newDeckName string, out io.Writer) (domain.Deck, app.ImportReport, error) {
	if deckName != "" && newDeckName != "" {
		return domain.Deck{}, app.ImportReport{}, fmt.Errorf("cannot use both --deck and --new; choose one")
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return domain.Deck{}, app.ImportReport{}, fmt.Errorf("read file: %w", err)
	}
	exp, err := export.UnmarshalExport(data)
	if err != nil {
		return domain.Deck{}, app.ImportReport{}, err
	}

	service := app.NewService(store)
	targetDeck, err := resolveTargetDeckForImport(ctx, store, service, exp, deckName, newDeckName)
	if err != nil {
		return domain.Deck{}, app.ImportReport{}, err
	}

	report, err := service.ImportCardsToDeckForUser(ctx, 0, targetDeck.ID, data)
	if err != nil {
		return domain.Deck{}, app.ImportReport{}, err
	}
	if out != nil {
		_, _ = fmt.Fprintf(out, "Added cards to %q (%s->%s).\n", targetDeck.Name, targetDeck.LanguageFrom, targetDeck.LanguageTo)
		_, _ = fmt.Fprintf(out, "Import summary: total=%d created=%d skipped_duplicates=%d failed=%d\n",
			report.Total, report.Created, report.SkippedDuplicates, report.Failed)
	}
	return *targetDeck, report, nil
}

func newDeckCmd(ctx *commandContext) *cobra.Command {
	deckCmd := &cobra.Command{
		Use:   "deck",
		Short: "Manage decks",
	}

	deckCmd.AddCommand(newDeckCreateCmd(ctx))
	deckCmd.AddCommand(newDeckListCmd(ctx))
	deckCmd.AddCommand(newDeckUseCmd(ctx))
	deckCmd.AddCommand(newDeckCurrentCmd(ctx))
	deckCmd.AddCommand(newDeckExportCmd(ctx))
	deckCmd.AddCommand(newDeckImportCmd(ctx))

	return deckCmd
}

func newDeckCreateCmd(ctx *commandContext) *cobra.Command {
	createCmd := &cobra.Command{
		Use:   "create <from> <to> <name...>",
		Short: "Create a new deck",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			languageFrom := args[0]
			languageTo := args[1]
			name := strings.Join(args[2:], " ")
			_, err := runDeckCreate(cmd.Context(), ctx.Store, languageFrom, languageTo, name, cmd.OutOrStdout())
			return err
		},
	}

	return createCmd
}

func newDeckListCmd(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all decks (CLI and bot-created)",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := runDeckList(cmd.Context(), ctx.Store, cmd.OutOrStdout())
			return err
		},
	}
}

func newDeckUseCmd(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name...>",
		Short: "Set active deck by exact name",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.Join(args, " ")
			_, err := runDeckUse(cmd.Context(), ctx.Store, name, cmd.OutOrStdout())
			return err
		},
	}
}

func newDeckCurrentCmd(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show active deck",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := runDeckCurrent(cmd.Context(), ctx.Store, cmd.OutOrStdout())
			return err
		},
	}
}

func newDeckExportCmd(ctx *commandContext) *cobra.Command {
	var deckName, outputPath string
	exportCmd := &cobra.Command{
		Use:   "export",
		Short: "Export deck to JSON file",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := runDeckExport(cmd.Context(), ctx.Store, deckName, outputPath, cmd.OutOrStdout())
			return err
		},
	}
	exportCmd.Flags().StringVar(&deckName, "deck", "", "Deck name to export (default: active deck)")
	exportCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output file path (default: stdout)")
	return exportCmd
}

func newDeckImportCmd(ctx *commandContext) *cobra.Command {
	var deckName, newDeckName string
	importCmd := &cobra.Command{
		Use:   "import <file.json>",
		Short: "Import cards from JSON file into a deck",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, err := runDeckImport(cmd.Context(), ctx.Store, args[0], deckName, newDeckName, cmd.OutOrStdout())
			return err
		},
	}
	importCmd.Flags().StringVar(&deckName, "deck", "", "Add cards to existing deck by name")
	importCmd.Flags().StringVar(&newDeckName, "new", "", "Create new deck with this name and add cards")
	return importCmd
}
