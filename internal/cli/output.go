package cli

import (
	"fmt"
	"io"
	"word-learning/internal/domain"
)

func printDecksAllTo(out io.Writer, decks []domain.Deck) {
	if out == nil {
		out = io.Discard
	}
	if len(decks) == 0 {
		_, _ = fmt.Fprintln(out, "No decks yet.")
		return
	}
	const ownerWidth = 22 // fits "Telegram (188037393)"
	_, _ = fmt.Fprintf(out, "%-4s%-*s%s\t%s\t%s\n", "ID", ownerWidth, "OWNER", "FROM", "TO", "NAME")
	for _, deck := range decks {
		_, _ = fmt.Fprintf(out, "%-4d%-*s%s\t%s\t%s\n", deck.ID, ownerWidth, formatDeckOwner(deck.TelegramUserID), deck.LanguageFrom, deck.LanguageTo, deck.Name)
	}
}

func formatDeckOwner(telegramUserID int64) string {
	if telegramUserID == 0 {
		return "CLI"
	}
	return fmt.Sprintf("Telegram (%d)", telegramUserID)
}

func printCardsTo(out io.Writer, cards []domain.Card) {
	if out == nil {
		out = io.Discard
	}
	if len(cards) == 0 {
		_, _ = fmt.Fprintln(out, "No cards found.")
		return
	}
	_, _ = fmt.Fprintln(out, "ID\tDECK\tSTATUS\tFRONT\tBACK\tPRONUNCIATION")
	for _, card := range cards {
		_, _ = fmt.Fprintf(out, "%d\t%d\t%s\t%s\t%s\t%s\n", card.ID, card.DeckID, card.Status, card.Front, card.Back, card.Pronunciation)
	}
}

func printCardDetailsTo(out io.Writer, card domain.Card) {
	if out == nil {
		out = io.Discard
	}
	_, _ = fmt.Fprintf(out, "Card ID: %d\n", card.ID)
	_, _ = fmt.Fprintf(out, "Front: %s\n", card.Front)
	_, _ = fmt.Fprintf(out, "Back: %s\n", card.Back)
	if card.Pronunciation != "" {
		_, _ = fmt.Fprintf(out, "Pronunciation: %s\n", card.Pronunciation)
	}
	if card.Conjugation != "" {
		_, _ = fmt.Fprintf(out, "Conjugation: %s\n", card.Conjugation)
	}
	if card.Example != "" {
		_, _ = fmt.Fprintf(out, "Example: %s\n", card.Example)
	}
	_, _ = fmt.Fprintf(out, "Status: %s\n", card.Status)
}
