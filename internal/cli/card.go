package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
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

	return cardCmd
}

func newCardAddCmd(ctx *commandContext) *cobra.Command {
	var deckID int64
	var front string
	var back string
	var description string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a card",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			card, err := service.AddCard(context.Background(), deckID, front, back, description)
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
	cmd.Flags().StringVar(&description, "description", "", "Optional note/example")
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
			cards, err := service.ListCards(context.Background(), deckID, status)
			if err != nil {
				return err
			}
			printCards(cards)
			return nil
		},
	}

	cmd.Flags().Int64Var(&deckID, "deck", 0, "Deck ID")
	cmd.Flags().StringVar(&status, "status", "", "Optional filter: active|snoozed|removed")
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
			if err := service.RemoveCard(context.Background(), cardID); err != nil {
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
		Short: "Restore a removed/snoozed card to active status",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			if err := service.RestoreCard(context.Background(), cardID); err != nil {
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
			card, err := service.NextCard(context.Background(), deckID)
			if err != nil {
				return err
			}
			if card == nil {
				fmt.Println("No available cards right now.")
				return nil
			}
			printCardDetails(*card)
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
		Short: "Snooze card for 24 hours",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			if err := service.RememberCard(context.Background(), cardID); err != nil {
				if errors.Is(err, app.ErrCardNotFound) {
					return fmt.Errorf("card %d not found", cardID)
				}
				return err
			}
			fmt.Printf("Card snoozed for 24h: id=%d\n", cardID)
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
		Short: "Keep card active in rotation",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := app.NewService(ctx.Store)
			if err := service.DontRememberCard(context.Background(), cardID); err != nil {
				if errors.Is(err, app.ErrCardNotFound) {
					return fmt.Errorf("card %d not found", cardID)
				}
				return err
			}
			fmt.Printf("Card set to active: id=%d\n", cardID)
			return nil
		},
	}

	cmd.Flags().Int64Var(&cardID, "id", 0, "Card ID")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
