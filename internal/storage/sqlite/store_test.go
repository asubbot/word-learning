package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"word-learning/internal/domain"
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

func mustCreateDeckForOwnerStoreTest(t *testing.T, store *Store, ctx context.Context, telegramUserID int64, name, from, to string) domain.Deck {
	t.Helper()
	deck, err := store.CreateDeckForOwner(ctx, telegramUserID, name, from, to)
	if err != nil {
		t.Fatalf("create deck for owner: %v", err)
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

//nolint:gocyclo // subtests only
func TestUserOverdueAndLastReview(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "reminder.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if err := store.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	now := time.Now().UTC()

	t.Run("no_decks_for_user", func(t *testing.T) {
		count, lastReview, err := store.UserOverdueAndLastReview(ctx, 999, now)
		if err != nil {
			t.Fatalf("UserOverdueAndLastReview: %v", err)
		}
		if count != 0 || lastReview != nil {
			t.Fatalf("expected 0, nil; got count=%d lastReview=%v", count, lastReview)
		}
	})

	t.Run("decks_but_no_cards", func(t *testing.T) {
		mustCreateDeckForOwnerStoreTest(t, store, ctx, 101, "Empty", "EN", "RU")
		count, lastReview, err := store.UserOverdueAndLastReview(ctx, 101, now)
		if err != nil {
			t.Fatalf("UserOverdueAndLastReview: %v", err)
		}
		if count != 0 || lastReview != nil {
			t.Fatalf("expected 0, nil; got count=%d lastReview=%v", count, lastReview)
		}
	})

	t.Run("overdue_count", func(t *testing.T) {
		deck := mustCreateDeckForOwnerStoreTest(t, store, ctx, 102, "Deck", "EN", "RU")
		mustCreateStoreCard(t, store, ctx, deck.ID, "a", "x", "", "", "")
		mustCreateStoreCard(t, store, ctx, deck.ID, "b", "y", "", "", "")
		mustCreateStoreCard(t, store, ctx, deck.ID, "c", "z", "", "", "")
		count, lastReview, err := store.UserOverdueAndLastReview(ctx, 102, now)
		if err != nil {
			t.Fatalf("UserOverdueAndLastReview: %v", err)
		}
		if count != 3 {
			t.Fatalf("expected overdue count 3; got %d", count)
		}
		if lastReview != nil {
			t.Fatalf("expected lastReview nil (never reviewed); got %v", lastReview)
		}
	})

	t.Run("last_review_time", func(t *testing.T) {
		deck := mustCreateDeckForOwnerStoreTest(t, store, ctx, 103, "Deck", "EN", "RU")
		card := mustCreateStoreCard(t, store, ctx, deck.ID, "word", "back", "", "", "")
		reviewedAt := now.Add(-2 * time.Hour)
		updated, err := store.UpdateCardSchedule(ctx, card.ID, now.Add(24*time.Hour), 86400, 2.5, 0, reviewedAt)
		if err != nil || !updated {
			t.Fatalf("UpdateCardSchedule: %v", err)
		}
		count, lastReview, err := store.UserOverdueAndLastReview(ctx, 103, now)
		if err != nil {
			t.Fatalf("UserOverdueAndLastReview: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected overdue 0 (card due in future); got %d", count)
		}
		if lastReview == nil || !lastReview.Equal(reviewedAt) {
			t.Fatalf("expected lastReview %v; got %v", reviewedAt, lastReview)
		}
	})

	t.Run("never_reviewed", func(t *testing.T) {
		deck := mustCreateDeckForOwnerStoreTest(t, store, ctx, 104, "Deck", "EN", "RU")
		mustCreateStoreCard(t, store, ctx, deck.ID, "x", "y", "", "", "")
		_, lastReview, err := store.UserOverdueAndLastReview(ctx, 104, now)
		if err != nil {
			t.Fatalf("UserOverdueAndLastReview: %v", err)
		}
		if lastReview != nil {
			t.Fatalf("expected lastReview nil; got %v", lastReview)
		}
	})

	t.Run("only_non_overdue_or_removed", func(t *testing.T) {
		deck := mustCreateDeckForOwnerStoreTest(t, store, ctx, 105, "Deck", "EN", "RU")
		card1 := mustCreateStoreCard(t, store, ctx, deck.ID, "p", "q", "", "", "")
		card2 := mustCreateStoreCard(t, store, ctx, deck.ID, "r", "s", "", "", "")
		mustSetStoreSchedule(t, store, ctx, card1.ID, now.Add(24*time.Hour), 86400, 2.5, 0, "postponed")
		mustSetStoreStatus(t, store, ctx, card2.ID, domain.CardStatusRemoved)
		count, _, err := store.UserOverdueAndLastReview(ctx, 105, now)
		if err != nil {
			t.Fatalf("UserOverdueAndLastReview: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected overdue 0; got %d", count)
		}
	})

	t.Run("boundary_now", func(t *testing.T) {
		deck := mustCreateDeckForOwnerStoreTest(t, store, ctx, 106, "Deck", "EN", "RU")
		card := mustCreateStoreCard(t, store, ctx, deck.ID, "bnd", "b", "", "", "")
		updated, err := store.UpdateCardSchedule(ctx, card.ID, now, 600, 2.5, 0, now.Add(-time.Hour))
		if err != nil || !updated {
			t.Fatalf("UpdateCardSchedule: %v", err)
		}
		count, _, err := store.UserOverdueAndLastReview(ctx, 106, now)
		if err != nil {
			t.Fatalf("UserOverdueAndLastReview: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected overdue 1 (next_due_at exactly now); got %d", count)
		}
	})
}
