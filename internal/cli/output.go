package cli

import (
	"fmt"
	"word-learning-cli/internal/domain"
)

func printDecks(decks []domain.Deck) {
	if len(decks) == 0 {
		fmt.Println("No decks yet.")
		return
	}

	fmt.Println("ID\tNAME\tFROM\tTO")
	for _, deck := range decks {
		fmt.Printf("%d\t%s\t%s\t%s\n", deck.ID, deck.Name, deck.LanguageFrom, deck.LanguageTo)
	}
}

func printCards(cards []domain.Card) {
	if len(cards) == 0 {
		fmt.Println("No cards found.")
		return
	}

	fmt.Println("ID\tDECK\tSTATUS\tFRONT\tBACK")
	for _, card := range cards {
		fmt.Printf("%d\t%d\t%s\t%s\t%s\n", card.ID, card.DeckID, card.Status, card.Front, card.Back)
	}
}

func printCardDetails(card domain.Card) {
	fmt.Printf("Card ID: %d\n", card.ID)
	fmt.Printf("Front: %s\n", card.Front)
	fmt.Printf("Back: %s\n", card.Back)
	if card.Description != "" {
		fmt.Printf("Description: %s\n", card.Description)
	}
	fmt.Printf("Status: %s\n", card.Status)
}
