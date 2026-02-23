package cli

import (
	"context"
	"fmt"

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
	var name string
	var languageFrom string
	var languageTo string

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new deck",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			deck, err := service.CreateDeck(context.Background(), name, languageFrom, languageTo)
			if err != nil {
				return err
			}
			fmt.Printf("Deck created: id=%d name=%q pair=%s->%s\n", deck.ID, deck.Name, deck.LanguageFrom, deck.LanguageTo)
			return nil
		},
	}

	createCmd.Flags().StringVar(&name, "name", "", "Deck name")
	createCmd.Flags().StringVar(&languageFrom, "from", "", "Source language code (e.g. EN)")
	createCmd.Flags().StringVar(&languageTo, "to", "", "Target language code (e.g. RU)")
	_ = createCmd.MarkFlagRequired("name")
	_ = createCmd.MarkFlagRequired("from")
	_ = createCmd.MarkFlagRequired("to")

	return createCmd
}

func newDeckListCmd(ctx *commandContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List decks",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			decks, err := service.ListDecks(context.Background())
			if err != nil {
				return err
			}
			printDecks(decks)
			return nil
		},
	}
}
