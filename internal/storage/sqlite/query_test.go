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
	postponedCard, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "b", Back: "b", Pronunciation: "/b/", Description: ""})
	if err != nil {
		t.Fatalf("CreateCard postponed: %v", err)
	}
	removedCard, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "c", Back: "c", Pronunciation: "/c/", Description: ""})
	if err != nil {
		t.Fatalf("CreateCard removed: %v", err)
	}

	future := time.Now().UTC().Add(2 * time.Hour)
	if updated, err := store.UpdateCardSchedule(ctx, postponedCard.ID, future, 600, 2.3, 1, time.Now().UTC()); err != nil || !updated {
		t.Fatalf("UpdateCardSchedule postponed: updated=%v err=%v", updated, err)
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
	if len(activeCards) != 2 {
		t.Fatalf("expected 2 active cards, got %#v", activeCards)
	}
	if activeCards[0].ID != activeCard.ID || activeCards[0].Pronunciation != "/a/" {
		t.Fatalf("unexpected first active card: %#v", activeCards[0])
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

	dueCard, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "a", Back: "a"})
	if err != nil {
		t.Fatalf("CreateCard due: %v", err)
	}
	postponedCard, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "b", Back: "b"})
	if err != nil {
		t.Fatalf("CreateCard postponed: %v", err)
	}
	removedCard, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "c", Back: "c"})
	if err != nil {
		t.Fatalf("CreateCard removed: %v", err)
	}

	now := time.Now().UTC()
	if updated, err := store.UpdateCardSchedule(ctx, dueCard.ID, now.Add(-time.Minute), 600, 2.3, 1, now); err != nil || !updated {
		t.Fatalf("UpdateCardSchedule due: updated=%v err=%v", updated, err)
	}
	if updated, err := store.UpdateCardSchedule(ctx, postponedCard.ID, now.Add(10*time.Minute), 600, 2.3, 1, now); err != nil || !updated {
		t.Fatalf("UpdateCardSchedule postponed: updated=%v err=%v", updated, err)
	}
	if updated, err := store.SetCardStatus(ctx, removedCard.ID, domain.CardStatusRemoved, nil); err != nil || !updated {
		t.Fatalf("SetCardStatus removed: updated=%v err=%v", updated, err)
	}

	stats, err := store.DeckCardStats(ctx, deck.ID, now)
	if err != nil {
		t.Fatalf("DeckCardStats: %v", err)
	}
	if stats.Active != 1 || stats.Postponed != 1 || stats.Total != 2 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

func TestNextCardForDeck_UsesDueDateOrder(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	deck, err := store.CreateDeck(ctx, "Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeck: %v", err)
	}

	first, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "first", Back: "one"})
	if err != nil {
		t.Fatalf("CreateCard first: %v", err)
	}
	second, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "second", Back: "two"})
	if err != nil {
		t.Fatalf("CreateCard second: %v", err)
	}

	if updated, err := store.UpdateCardSchedule(ctx, first.ID, now.Add(5*time.Minute), 300, 2.5, 0, now); err != nil || !updated {
		t.Fatalf("UpdateCardSchedule first: updated=%v err=%v", updated, err)
	}
	if updated, err := store.UpdateCardSchedule(ctx, second.ID, now.Add(-2*time.Minute), 300, 2.5, 0, now); err != nil || !updated {
		t.Fatalf("UpdateCardSchedule second: updated=%v err=%v", updated, err)
	}

	next, err := store.NextCardForDeck(ctx, deck.ID, now)
	if err != nil {
		t.Fatalf("NextCardForDeck: %v", err)
	}
	if next == nil || next.ID != second.ID {
		t.Fatalf("expected due card %d, got %#v", second.ID, next)
	}
}

func TestInitSchema_MigratesLegacySnoozedToActiveDue(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()

	if _, err := store.DB().ExecContext(ctx, `
CREATE TABLE decks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  language_from TEXT NOT NULL,
  language_to TEXT NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`); err != nil {
		t.Fatalf("create legacy decks: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `
CREATE TABLE cards (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  deck_id INTEGER NOT NULL,
  front TEXT NOT NULL,
  back TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'snoozed', 'removed')),
  snoozed_until DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);`); err != nil {
		t.Fatalf("create legacy cards: %v", err)
	}

	if _, err := store.DB().ExecContext(ctx, `INSERT INTO decks (id, name, language_from, language_to) VALUES (1, 'Legacy', 'EN', 'RU')`); err != nil {
		t.Fatalf("insert deck: %v", err)
	}
	future := time.Now().UTC().Add(2 * time.Hour)
	if _, err := store.DB().ExecContext(ctx, `
INSERT INTO cards (deck_id, front, back, status, snoozed_until)
VALUES (1, 'legacy', 'наследие', 'snoozed', ?)
`, future); err != nil {
		t.Fatalf("insert legacy card: %v", err)
	}

	if err := store.InitSchema(ctx); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}

	card, err := store.GetCardByID(ctx, 1)
	if err != nil {
		t.Fatalf("GetCardByID: %v", err)
	}
	if card == nil {
		t.Fatal("expected migrated card")
	}
	if card.Status != domain.CardStatusActive {
		t.Fatalf("expected status active, got %s", card.Status)
	}
	if card.NextDueAt.Before(future.Add(-2 * time.Second)) {
		t.Fatalf("expected next_due_at to preserve snoozed_until, got %v (< %v)", card.NextDueAt, future)
	}
	if card.SnoozedUntil != nil {
		t.Fatalf("expected snoozed_until to be cleared, got %v", card.SnoozedUntil)
	}
}
