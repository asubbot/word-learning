package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"
	"word-learning/internal/storage/sqlite"
)

//nolint:gocyclo // subtests only
func TestReminderEligible(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "reminder-eligible.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	if err := store.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	svc := NewService(store)
	const minOverdue = 10
	const minHours = 12

	now := time.Now().UTC()

	t.Run("zero_overdue", func(t *testing.T) {
		deck, err := store.CreateDeckForOwner(ctx, 201, "D", "EN", "RU")
		if err != nil {
			t.Fatalf("CreateDeckForOwner: %v", err)
		}
		if _, err := store.CreateCard(ctx, sqlite.CardCreateParams{DeckID: deck.ID, Front: "a", Back: "x"}); err != nil {
			t.Fatalf("CreateCard: %v", err)
		}
		eligible, count, err := svc.ReminderEligible(ctx, 201, now, minOverdue, minHours)
		if err != nil {
			t.Fatalf("ReminderEligible: %v", err)
		}
		if eligible {
			t.Fatalf("expected not eligible (only 1 overdue); got eligible")
		}
		if count != 1 {
			t.Fatalf("expected count 1; got %d", count)
		}
	})

	t.Run("below_threshold", func(t *testing.T) {
		deck, err := store.CreateDeckForOwner(ctx, 202, "D", "EN", "RU")
		if err != nil {
			t.Fatalf("CreateDeckForOwner: %v", err)
		}
		for i := 0; i < 5; i++ {
			if _, err := store.CreateCard(ctx, sqlite.CardCreateParams{DeckID: deck.ID, Front: "f", Back: "b"}); err != nil {
				t.Fatalf("CreateCard: %v", err)
			}
		}
		eligible, count, err := svc.ReminderEligible(ctx, 202, now, minOverdue, minHours)
		if err != nil {
			t.Fatalf("ReminderEligible: %v", err)
		}
		if eligible {
			t.Fatalf("expected not eligible (5 < 10); got eligible")
		}
		if count != 5 {
			t.Fatalf("expected count 5; got %d", count)
		}
	})

	t.Run("10_plus_overdue_never_reviewed", func(t *testing.T) {
		deck, err := store.CreateDeckForOwner(ctx, 203, "D", "EN", "RU")
		if err != nil {
			t.Fatalf("CreateDeckForOwner: %v", err)
		}
		for i := 0; i < 12; i++ {
			if _, err := store.CreateCard(ctx, sqlite.CardCreateParams{DeckID: deck.ID, Front: "f", Back: "b"}); err != nil {
				t.Fatalf("CreateCard: %v", err)
			}
		}
		eligible, count, err := svc.ReminderEligible(ctx, 203, now, minOverdue, minHours)
		if err != nil {
			t.Fatalf("ReminderEligible: %v", err)
		}
		if !eligible {
			t.Fatalf("expected eligible (12 overdue, never reviewed); got not eligible")
		}
		if count != 12 {
			t.Fatalf("expected count 12; got %d", count)
		}
	})

	t.Run("10_plus_overdue_last_review_11h_ago", func(t *testing.T) {
		deck, err := store.CreateDeckForOwner(ctx, 204, "D", "EN", "RU")
		if err != nil {
			t.Fatalf("CreateDeckForOwner: %v", err)
		}
		for i := 0; i < 10; i++ {
			if _, err := store.CreateCard(ctx, sqlite.CardCreateParams{DeckID: deck.ID, Front: "f", Back: "b"}); err != nil {
				t.Fatalf("CreateCard: %v", err)
			}
		}
		cards, err := store.ListCards(ctx, deck.ID, nil)
		if err != nil {
			t.Fatalf("ListCards: %v", err)
		}
		lastReview := now.Add(-11 * time.Hour)
		if ok, err := store.UpdateCardSchedule(ctx, cards[0].ID, now.Add(-time.Hour), 600, 2.5, 0, lastReview); err != nil || !ok {
			t.Fatalf("UpdateCardSchedule: %v", err)
		}
		eligible, _, err := svc.ReminderEligible(ctx, 204, now, minOverdue, minHours)
		if err != nil {
			t.Fatalf("ReminderEligible: %v", err)
		}
		if eligible {
			t.Fatalf("expected not eligible (last review 11h ago); got eligible")
		}
	})

	t.Run("10_plus_overdue_last_review_13h_ago", func(t *testing.T) {
		deck, err := store.CreateDeckForOwner(ctx, 205, "D", "EN", "RU")
		if err != nil {
			t.Fatalf("CreateDeckForOwner: %v", err)
		}
		for i := 0; i < 10; i++ {
			if _, err := store.CreateCard(ctx, sqlite.CardCreateParams{DeckID: deck.ID, Front: "f", Back: "b"}); err != nil {
				t.Fatalf("CreateCard: %v", err)
			}
		}
		cards, err := store.ListCards(ctx, deck.ID, nil)
		if err != nil {
			t.Fatalf("ListCards: %v", err)
		}
		lastReview := now.Add(-13 * time.Hour)
		if ok, err := store.UpdateCardSchedule(ctx, cards[0].ID, now.Add(-time.Hour), 600, 2.5, 0, lastReview); err != nil || !ok {
			t.Fatalf("UpdateCardSchedule: %v", err)
		}
		eligible, count, err := svc.ReminderEligible(ctx, 205, now, minOverdue, minHours)
		if err != nil {
			t.Fatalf("ReminderEligible: %v", err)
		}
		if !eligible {
			t.Fatalf("expected eligible (last review 13h ago); got not eligible")
		}
		if count < 10 {
			t.Fatalf("expected count >= 10; got %d", count)
		}
	})

	t.Run("boundary_12h", func(t *testing.T) {
		deck, err := store.CreateDeckForOwner(ctx, 206, "D", "EN", "RU")
		if err != nil {
			t.Fatalf("CreateDeckForOwner: %v", err)
		}
		for i := 0; i < 10; i++ {
			if _, err := store.CreateCard(ctx, sqlite.CardCreateParams{DeckID: deck.ID, Front: "f", Back: "b"}); err != nil {
				t.Fatalf("CreateCard: %v", err)
			}
		}
		cards, err := store.ListCards(ctx, deck.ID, nil)
		if err != nil {
			t.Fatalf("ListCards: %v", err)
		}
		lastReview := now.Add(-12*time.Hour - time.Minute)
		if ok, err := store.UpdateCardSchedule(ctx, cards[0].ID, now.Add(-time.Hour), 600, 2.5, 0, lastReview); err != nil || !ok {
			t.Fatalf("UpdateCardSchedule: %v", err)
		}
		eligible, _, err := svc.ReminderEligible(ctx, 206, now, minOverdue, minHours)
		if err != nil {
			t.Fatalf("ReminderEligible: %v", err)
		}
		if !eligible {
			t.Fatalf("expected eligible (last review 12h+ ago); got not eligible")
		}
	})
}
