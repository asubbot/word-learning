package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"word-learning-cli/internal/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "sqlite-test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	return store
}

func TestDeckQueries(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	deck, err := store.CreateDeck(ctx, "  Deck Name  ", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeck: %v", err)
	}

	exists, err := store.DeckExists(ctx, deck.ID)
	if err != nil {
		t.Fatalf("DeckExists: %v", err)
	}
	if !exists {
		t.Fatal("expected existing deck")
	}

	exists, err = store.DeckExists(ctx, deck.ID+100)
	if err != nil {
		t.Fatalf("DeckExists missing: %v", err)
	}
	if exists {
		t.Fatal("did not expect existing deck for unknown id")
	}

	decks, err := store.ListDecks(ctx)
	if err != nil {
		t.Fatalf("ListDecks: %v", err)
	}
	if len(decks) != 1 {
		t.Fatalf("expected 1 deck, got %d", len(decks))
	}
	if decks[0].Name != "Deck Name" {
		t.Fatalf("expected trimmed deck name, got %q", decks[0].Name)
	}
}

func TestListCardsWithStatusFilter(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	deck, err := store.CreateDeck(ctx, "Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeck: %v", err)
	}

	activeCard, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "a", Back: "a", Pronunciation: "/a/", Description: ""})
	if err != nil {
		t.Fatalf("CreateCard active: %v", err)
	}
	snoozedCard, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "b", Back: "b", Pronunciation: "/b/", Description: ""})
	if err != nil {
		t.Fatalf("CreateCard snoozed: %v", err)
	}
	removedCard, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "c", Back: "c", Pronunciation: "/c/", Description: ""})
	if err != nil {
		t.Fatalf("CreateCard removed: %v", err)
	}

	future := time.Now().UTC().Add(2 * time.Hour)
	if updated, err := store.SetCardStatus(ctx, snoozedCard.ID, domain.CardStatusSnoozed, &future); err != nil || !updated {
		t.Fatalf("SetCardStatus snoozed: updated=%v err=%v", updated, err)
	}
	if updated, err := store.SetCardStatus(ctx, removedCard.ID, domain.CardStatusRemoved, nil); err != nil || !updated {
		t.Fatalf("SetCardStatus removed: updated=%v err=%v", updated, err)
	}

	allCards, err := store.ListCards(ctx, deck.ID, nil)
	if err != nil {
		t.Fatalf("ListCards all: %v", err)
	}
	if len(allCards) != 3 {
		t.Fatalf("expected 3 cards, got %d", len(allCards))
	}

	activeStatus := domain.CardStatusActive
	activeCards, err := store.ListCards(ctx, deck.ID, &activeStatus)
	if err != nil {
		t.Fatalf("ListCards active: %v", err)
	}
	if len(activeCards) != 1 || activeCards[0].ID != activeCard.ID {
		t.Fatalf("unexpected active cards: %#v", activeCards)
	}
	if activeCards[0].Pronunciation != "/a/" {
		t.Fatalf("unexpected pronunciation for active card: %q", activeCards[0].Pronunciation)
	}

	snoozedStatus := domain.CardStatusSnoozed
	snoozedCards, err := store.ListCards(ctx, deck.ID, &snoozedStatus)
	if err != nil {
		t.Fatalf("ListCards snoozed: %v", err)
	}
	if len(snoozedCards) != 1 || snoozedCards[0].ID != snoozedCard.ID {
		t.Fatalf("unexpected snoozed cards: %#v", snoozedCards)
	}
	if snoozedCards[0].Pronunciation != "/b/" {
		t.Fatalf("unexpected pronunciation for snoozed card: %q", snoozedCards[0].Pronunciation)
	}
	if snoozedCards[0].SnoozedUntil == nil {
		t.Fatal("expected snoozed_until for snoozed card")
	}

	removedStatus := domain.CardStatusRemoved
	removedCards, err := store.ListCards(ctx, deck.ID, &removedStatus)
	if err != nil {
		t.Fatalf("ListCards removed: %v", err)
	}
	if len(removedCards) != 1 || removedCards[0].ID != removedCard.ID {
		t.Fatalf("unexpected removed cards: %#v", removedCards)
	}
}

func TestDeckCardStats(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	deck, err := store.CreateDeck(ctx, "Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeck: %v", err)
	}

	activeCard, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "a", Back: "a"})
	if err != nil {
		t.Fatalf("CreateCard active: %v", err)
	}
	snoozedCard, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "b", Back: "b"})
	if err != nil {
		t.Fatalf("CreateCard snoozed: %v", err)
	}
	removedCard, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "c", Back: "c"})
	if err != nil {
		t.Fatalf("CreateCard removed: %v", err)
	}

	future := time.Now().UTC().Add(2 * time.Hour)
	if updated, err := store.SetCardStatus(ctx, snoozedCard.ID, domain.CardStatusSnoozed, &future); err != nil || !updated {
		t.Fatalf("SetCardStatus snoozed: updated=%v err=%v", updated, err)
	}
	if updated, err := store.SetCardStatus(ctx, removedCard.ID, domain.CardStatusRemoved, nil); err != nil || !updated {
		t.Fatalf("SetCardStatus removed: updated=%v err=%v", updated, err)
	}

	stats, err := store.DeckCardStats(ctx, deck.ID)
	if err != nil {
		t.Fatalf("DeckCardStats: %v", err)
	}

	if stats.Active != 1 || stats.Snoozed != 1 || stats.Total != 3 {
		t.Fatalf("unexpected stats: %#v", stats)
	}

	if updated, err := store.SetCardStatus(ctx, activeCard.ID, domain.CardStatusRemoved, nil); err != nil || !updated {
		t.Fatalf("SetCardStatus active->removed: updated=%v err=%v", updated, err)
	}

	stats, err = store.DeckCardStats(ctx, deck.ID)
	if err != nil {
		t.Fatalf("DeckCardStats after update: %v", err)
	}
	if stats.Active != 0 || stats.Snoozed != 1 || stats.Total != 3 {
		t.Fatalf("unexpected stats after update: %#v", stats)
	}
}
