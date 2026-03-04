package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"word-learning/internal/app"
	"word-learning/internal/domain"
	"word-learning/internal/storage/sqlite"
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

func newDeckCmd(ctx *commandContext) *cobra.Command {
	deckCmd := &cobra.Command{
		Use:   "deck",
		Short: "Manage decks",
	}

	deckCmd.AddCommand(newDeckCreateCmd(ctx))
	deckCmd.AddCommand(newDeckListCmd(ctx))
	deckCmd.AddCommand(newDeckUseCmd(ctx))
	deckCmd.AddCommand(newDeckCurrentCmd(ctx))

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
