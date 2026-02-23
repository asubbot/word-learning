package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"word-learning-cli/internal/domain"
)

func TestNextCardForDeck_RespectsDueAndRemoved(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if err := store.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	deck, err := store.CreateDeck(ctx, "Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("create deck: %v", err)
	}

	card, err := store.CreateCard(ctx, CardCreateParams{
		DeckID:        deck.ID,
		Front:         "banished",
		Back:          "изгнанный",
		Pronunciation: "/banished/",
		Description:   "sample",
	})
	if err != nil {
		t.Fatalf("create card: %v", err)
	}

	next, err := store.NextCardForDeck(ctx, deck.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("next card active: %v", err)
	}
	if next == nil || next.ID != card.ID {
		t.Fatalf("expected card %d, got %#v", card.ID, next)
	}

	future := time.Now().UTC().Add(24 * time.Hour)
	updated, err := store.UpdateCardSchedule(ctx, card.ID, future, 86400, 2.5, 0, time.Now().UTC())
	if err != nil || !updated {
		t.Fatalf("set postponed schedule: updated=%v err=%v", updated, err)
	}

	next, err = store.NextCardForDeck(ctx, deck.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("next card postponed: %v", err)
	}
	if next != nil {
		t.Fatalf("expected nil while card is postponed, got %#v", next)
	}

	past := time.Now().UTC().Add(-time.Hour)
	updated, err = store.SetCardActiveNow(ctx, card.ID, past)
	if err != nil || !updated {
		t.Fatalf("set card due in the past: updated=%v err=%v", updated, err)
	}

	next, err = store.NextCardForDeck(ctx, deck.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("next card after due time: %v", err)
	}
	if next == nil || next.ID != card.ID {
		t.Fatalf("expected card %d after due time, got %#v", card.ID, next)
	}

	updated, err = store.SetCardStatus(ctx, card.ID, domain.CardStatusRemoved, nil)
	if err != nil || !updated {
		t.Fatalf("set removed status: updated=%v err=%v", updated, err)
	}

	next, err = store.NextCardForDeck(ctx, deck.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("next card removed: %v", err)
	}
	if next != nil {
		t.Fatalf("expected nil for removed card, got %#v", next)
	}
}
