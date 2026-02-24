package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"word-learning-cli/internal/app"
)

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

			service := app.NewService(ctx.Store)
			deck, err := service.CreateDeck(context.Background(), name, languageFrom, languageTo)
			if err != nil {
				return err
			}
			fmt.Printf("Deck created: id=%d name=%q pair=%s->%s\n", deck.ID, deck.Name, deck.LanguageFrom, deck.LanguageTo)
			return nil
		},
	}

	return createCmd
}

func newDeckListCmd(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all decks (CLI and bot-created)",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			decks, err := service.ListDecksAll(context.Background())
			if err != nil {
				return err
			}
			printDecksAll(decks)
			return nil
		},
	}
}

func newDeckUseCmd(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name...>",
		Short: "Set active deck by exact name",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			name := strings.Join(args, " ")
			result, err := service.DeckUseForUser(context.Background(), 0, name)
			if err != nil {
				if errors.Is(err, app.ErrDeckNameAmbiguous) {
					fmt.Println("Deck name is ambiguous. Candidates:")
					for _, d := range result.Candidates {
						fmt.Printf("- %s (%s->%s)\n", d.Name, d.LanguageFrom, d.LanguageTo)
					}
					return fmt.Errorf("please retry with exact deck name")
				}
				return err
			}
			if result.Deck == nil {
				return fmt.Errorf("failed to set active deck")
			}
			fmt.Printf("Active deck: %s (%s->%s)\n", result.Deck.Name, result.Deck.LanguageFrom, result.Deck.LanguageTo)
			return nil
		},
	}
}

func newDeckCurrentCmd(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show active deck",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			deck, err := service.DeckCurrentForUser(context.Background(), 0)
			if err != nil {
				return err
			}
			if deck == nil {
				return fmt.Errorf("active deck is not set; run 'deck use <name...>'")
			}
			fmt.Printf("Active deck: %s (%s->%s)\n", deck.Name, deck.LanguageFrom, deck.LanguageTo)
			return nil
		},
	}
}
