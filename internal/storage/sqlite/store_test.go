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

	deck := mustCreateDeckForStoreTest(t, store, ctx, "Deck", "EN", "RU")
	card := mustCreateStoreCard(t, store, ctx, deck.ID, "banished", "exiled", "/banished/", "sample", "")

	assertStoreNextCardID(t, store, ctx, deck.ID, card.ID, "active")
	mustSetStoreSchedule(t, store, ctx, card.ID, time.Now().UTC().Add(24*time.Hour), 86400, 2.5, 0, "set postponed schedule")
	assertStoreNoNextCard(t, store, ctx, deck.ID, "postponed")
	mustSetCardActiveNow(t, store, ctx, card.ID, time.Now().UTC().Add(-time.Hour))
	assertStoreNextCardID(t, store, ctx, deck.ID, card.ID, "after due time")
	mustSetStoreStatus(t, store, ctx, card.ID, domain.CardStatusRemoved)
	assertStoreNoNextCard(t, store, ctx, deck.ID, "removed")
}

func mustCreateDeckForStoreTest(t *testing.T, store *Store, ctx context.Context, name, from, to string) domain.Deck {
	t.Helper()
	deck, err := store.CreateDeck(ctx, name, from, to)
	if err != nil {
		t.Fatalf("create deck: %v", err)
	}
	return deck
}

func mustCreateStoreCard(t *testing.T, store *Store, ctx context.Context, deckID int64, front, back, pron, example, conjugation string) domain.Card {
	t.Helper()
	card, err := store.CreateCard(ctx, CardCreateParams{
		DeckID:        deckID,
		Front:         front,
		Back:          back,
		Pronunciation: pron,
		Example:       example,
		Conjugation:   conjugation,
	})
	if err != nil {
		t.Fatalf("create card: %v", err)
	}
	return card
}

func assertStoreNextCardID(t *testing.T, store *Store, ctx context.Context, deckID, wantID int64, label string) {
	t.Helper()
	next, err := store.NextCardForDeck(ctx, deckID, time.Now().UTC())
	if err != nil {
		t.Fatalf("next card %s: %v", label, err)
	}
	if next == nil || next.ID != wantID {
		t.Fatalf("expected card %d (%s), got %#v", wantID, label, next)
	}
}

func assertStoreNoNextCard(t *testing.T, store *Store, ctx context.Context, deckID int64, label string) {
	t.Helper()
	next, err := store.NextCardForDeck(ctx, deckID, time.Now().UTC())
	if err != nil {
		t.Fatalf("next card %s: %v", label, err)
	}
	if next != nil {
		t.Fatalf("expected nil next card (%s), got %#v", label, next)
	}
}

func mustSetStoreSchedule(t *testing.T, store *Store, ctx context.Context, cardID int64, due time.Time, interval int64, ease float64, lapses int64, label string) {
	t.Helper()
	updated, err := store.UpdateCardSchedule(ctx, cardID, due, interval, ease, lapses, time.Now().UTC())
	if err != nil || !updated {
		t.Fatalf("%s: updated=%v err=%v", label, updated, err)
	}
}

func mustSetCardActiveNow(t *testing.T, store *Store, ctx context.Context, cardID int64, due time.Time) {
	t.Helper()
	updated, err := store.SetCardActiveNow(ctx, cardID, due)
	if err != nil || !updated {
		t.Fatalf("set card active now: updated=%v err=%v", updated, err)
	}
}

func mustSetStoreStatus(t *testing.T, store *Store, ctx context.Context, cardID int64, status domain.CardStatus) {
	t.Helper()
	updated, err := store.SetCardStatus(ctx, cardID, status)
	if err != nil || !updated {
		t.Fatalf("set card status: updated=%v err=%v", updated, err)
	}
}
