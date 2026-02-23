package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"word-learning-cli/internal/domain"
)

func TestNextCardForDeck_RespectsStatuses(t *testing.T) {
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
		DeckID:      deck.ID,
		Front:       "banished",
		Back:        "изгнанный",
		Description: "sample",
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
	updated, err := store.SetCardStatus(ctx, card.ID, domain.CardStatusSnoozed, &future)
	if err != nil || !updated {
		t.Fatalf("set snoozed status: updated=%v err=%v", updated, err)
	}

	next, err = store.NextCardForDeck(ctx, deck.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("next card snoozed: %v", err)
	}
	if next != nil {
		t.Fatalf("expected nil while card is snoozed, got %#v", next)
	}

	past := time.Now().UTC().Add(-time.Hour)
	updated, err = store.SetCardStatus(ctx, card.ID, domain.CardStatusSnoozed, &past)
	if err != nil || !updated {
		t.Fatalf("set past snoozed_until: updated=%v err=%v", updated, err)
	}

	next, err = store.NextCardForDeck(ctx, deck.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("next card expired snooze: %v", err)
	}
	if next == nil || next.ID != card.ID {
		t.Fatalf("expected card %d after snooze expiry, got %#v", card.ID, next)
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
