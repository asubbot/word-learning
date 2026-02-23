package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"word-learning-cli/internal/storage/sqlite"
)

func newTestService(t *testing.T) (*Service, *sqlite.Store) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "service-test.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	return NewService(store), store
}

func mustCreateDeck(t *testing.T, svc *Service) int64 {
	t.Helper()

	deck, err := svc.CreateDeck(context.Background(), "English Basics", "en", "ru")
	if err != nil {
		t.Fatalf("create deck: %v", err)
	}
	return deck.ID
}

func TestServiceDeckFlow(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()

	deck, err := svc.CreateDeck(ctx, "  Basic Deck  ", "en", "ru")
	if err != nil {
		t.Fatalf("CreateDeck: %v", err)
	}

	if deck.Name != "Basic Deck" {
		t.Fatalf("unexpected deck name: %q", deck.Name)
	}
	if deck.LanguageFrom != "EN" || deck.LanguageTo != "RU" {
		t.Fatalf("unexpected language pair: %s -> %s", deck.LanguageFrom, deck.LanguageTo)
	}

	decks, err := svc.ListDecks(ctx)
	if err != nil {
		t.Fatalf("ListDecks: %v", err)
	}
	if len(decks) != 1 {
		t.Fatalf("expected 1 deck, got %d", len(decks))
	}
}

func TestServiceDeckValidation(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.CreateDeck(ctx, " ", "en", "ru"); err == nil {
		t.Fatal("expected error for empty name")
	}
	if _, err := svc.CreateDeck(ctx, "Deck", "e1", "ru"); err == nil {
		t.Fatal("expected error for invalid from language")
	}
	if _, err := svc.CreateDeck(ctx, "Deck", "en", "r1"); err == nil {
		t.Fatal("expected error for invalid to language")
	}
	if _, err := svc.CreateDeck(ctx, "Deck", "en", "en"); err == nil {
		t.Fatal("expected error for same language pair")
	}
}

func TestServiceCardLifecycle(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()
	deckID := mustCreateDeck(t, svc)

	card, err := svc.AddCard(ctx, deckID, " banished ", " exiled ", " /banished/ ", "  sample ")
	if err != nil {
		t.Fatalf("AddCard: %v", err)
	}
	if card.Front != "banished" || card.Back != "exiled" || card.Pronunciation != "/banished/" || card.Description != "sample" {
		t.Fatalf("unexpected trimmed values: %#v", card)
	}

	next, err := svc.NextCard(ctx, deckID)
	if err != nil {
		t.Fatalf("NextCard active: %v", err)
	}
	if next == nil || next.ID != card.ID {
		t.Fatalf("expected next card %d, got %#v", card.ID, next)
	}

	if err := svc.RememberCard(ctx, card.ID); err != nil {
		t.Fatalf("RememberCard: %v", err)
	}

	next, err = svc.NextCard(ctx, deckID)
	if err != nil {
		t.Fatalf("NextCard after remember: %v", err)
	}
	if next != nil {
		t.Fatalf("expected no card while due date is in future, got %#v", next)
	}

	stored, err := store.GetCardByID(ctx, card.ID)
	if err != nil {
		t.Fatalf("GetCardByID after remember: %v", err)
	}
	if stored == nil {
		t.Fatal("expected card after remember")
	}
	if stored.IntervalSec < 86400 {
		t.Fatalf("expected interval >= 1 day, got %d", stored.IntervalSec)
	}
	if stored.LastReviewedAt == nil {
		t.Fatal("expected last_reviewed_at after remember")
	}
	if !stored.NextDueAt.After(time.Now().UTC()) {
		t.Fatalf("expected future next_due_at, got %v", stored.NextDueAt)
	}

	_, stats, err := svc.NextCardWithStats(ctx, deckID)
	if err != nil {
		t.Fatalf("NextCardWithStats after remember: %v", err)
	}
	if stats.Active != 0 || stats.Postponed != 1 || stats.Total != 1 {
		t.Fatalf("unexpected stats after remember: %#v", stats)
	}

	if err := svc.DontRememberCard(ctx, card.ID); err != nil {
		t.Fatalf("DontRememberCard: %v", err)
	}

	next, err = svc.NextCard(ctx, deckID)
	if err != nil {
		t.Fatalf("NextCard after dont-remember: %v", err)
	}
	if next != nil {
		t.Fatalf("expected no card immediately after dont-remember short interval, got %#v", next)
	}

	stored, err = store.GetCardByID(ctx, card.ID)
	if err != nil {
		t.Fatalf("GetCardByID after dont-remember: %v", err)
	}
	if stored.Lapses != 1 {
		t.Fatalf("expected lapses=1, got %d", stored.Lapses)
	}
	if stored.IntervalSec != 600 {
		t.Fatalf("expected interval_sec=600, got %d", stored.IntervalSec)
	}

	if err := svc.RemoveCard(ctx, card.ID); err != nil {
		t.Fatalf("RemoveCard: %v", err)
	}

	cards, err := svc.ListCards(ctx, deckID, "removed")
	if err != nil {
		t.Fatalf("ListCards removed: %v", err)
	}
	if len(cards) != 1 || cards[0].ID != card.ID {
		t.Fatalf("expected removed card %d, got %#v", card.ID, cards)
	}

	_, stats, err = svc.NextCardWithStats(ctx, deckID)
	if err != nil {
		t.Fatalf("NextCardWithStats after remove: %v", err)
	}
	if stats.Active != 0 || stats.Postponed != 0 || stats.Total != 0 {
		t.Fatalf("unexpected stats after remove: %#v", stats)
	}

	if err := svc.RestoreCard(ctx, card.ID); err != nil {
		t.Fatalf("RestoreCard: %v", err)
	}

	next, err = svc.NextCard(ctx, deckID)
	if err != nil {
		t.Fatalf("NextCard after restore: %v", err)
	}
	if next == nil || next.ID != card.ID {
		t.Fatalf("expected restored card %d to be due now, got %#v", card.ID, next)
	}
}

func TestServiceCardValidationAndNotFound(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()
	deckID := mustCreateDeck(t, svc)

	if _, err := svc.AddCard(ctx, 0, "front", "back", "/f/", "desc"); err == nil {
		t.Fatal("expected error for invalid deck id")
	}
	if _, err := svc.AddCard(ctx, deckID, " ", "back", "/f/", "desc"); err == nil {
		t.Fatal("expected error for empty front")
	}
	if _, err := svc.AddCard(ctx, deckID, "front", " ", "/f/", "desc"); err == nil {
		t.Fatal("expected error for empty back")
	}
	if _, err := svc.AddCard(ctx, 999, "front", "back", "/f/", "desc"); err == nil {
		t.Fatal("expected error for unknown deck")
	}

	if _, err := svc.ListCards(ctx, deckID, "wrong"); err == nil {
		t.Fatal("expected error for invalid status")
	}
	if _, err := svc.NextCard(ctx, 0); err == nil {
		t.Fatal("expected error for invalid deck id in NextCard")
	}

	if err := svc.RemoveCard(ctx, 9999); !errors.Is(err, ErrCardNotFound) {
		t.Fatalf("expected ErrCardNotFound, got %v", err)
	}
	if err := svc.RestoreCard(ctx, 9999); !errors.Is(err, ErrCardNotFound) {
		t.Fatalf("expected ErrCardNotFound, got %v", err)
	}
	if err := svc.RememberCard(ctx, 9999); !errors.Is(err, ErrCardNotFound) {
		t.Fatalf("expected ErrCardNotFound, got %v", err)
	}
	if err := svc.DontRememberCard(ctx, 9999); !errors.Is(err, ErrCardNotFound) {
		t.Fatalf("expected ErrCardNotFound, got %v", err)
	}
}

func TestNextRememberIntervalSec(t *testing.T) {
	t.Parallel()

	if got := nextRememberIntervalSec(0, 2.5); got != 86400 {
		t.Fatalf("expected 86400 for first remember, got %d", got)
	}
	if got := nextRememberIntervalSec(86400, 2.5); got != 216000 {
		t.Fatalf("expected grown interval, got %d", got)
	}
	if got := nextRememberIntervalSec(100, 1.1); got != 86400 {
		t.Fatalf("expected minimum one day, got %d", got)
	}
}

func TestMaxEase(t *testing.T) {
	t.Parallel()

	if got := maxEase(0); got != 2.5 {
		t.Fatalf("expected default ease 2.5, got %f", got)
	}
	if got := maxEase(2.3); got != 2.3 {
		t.Fatalf("expected ease 2.3, got %f", got)
	}
}
