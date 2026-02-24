package cli

import (
	"context"
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
