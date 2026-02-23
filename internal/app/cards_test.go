package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"word-learning-cli/internal/domain"
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

	svc, _ := newTestService(t)
	ctx := context.Background()
	deckID := mustCreateDeck(t, svc)

	card, err := svc.AddCard(ctx, deckID, " banished ", " изгнанный ", " /banished/ ", "  sample ")
	if err != nil {
		t.Fatalf("AddCard: %v", err)
	}
	if card.Front != "banished" || card.Back != "изгнанный" || card.Pronunciation != "/banished/" || card.Description != "sample" {
		t.Fatalf("unexpected trimmed values: %#v", card)
	}

	cards, err := svc.ListCards(ctx, deckID, "active")
	if err != nil {
		t.Fatalf("ListCards active: %v", err)
	}
	if len(cards) != 1 || cards[0].ID != card.ID {
		t.Fatalf("expected active card %d, got %#v", card.ID, cards)
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
		t.Fatalf("expected no card while snoozed, got %#v", next)
	}

	cards, err = svc.ListCards(ctx, deckID, "snoozed")
	if err != nil {
		t.Fatalf("ListCards snoozed: %v", err)
	}
	if len(cards) != 1 || cards[0].SnoozedUntil == nil {
		t.Fatalf("expected one snoozed card with snoozed_until, got %#v", cards)
	}

	if err := svc.DontRememberCard(ctx, card.ID); err != nil {
		t.Fatalf("DontRememberCard: %v", err)
	}

	next, err = svc.NextCard(ctx, deckID)
	if err != nil {
		t.Fatalf("NextCard after dont-remember: %v", err)
	}
	if next == nil || next.ID != card.ID || next.Status != domain.CardStatusActive {
		t.Fatalf("expected active card %d, got %#v", card.ID, next)
	}

	if err := svc.RemoveCard(ctx, card.ID); err != nil {
		t.Fatalf("RemoveCard: %v", err)
	}

	cards, err = svc.ListCards(ctx, deckID, "removed")
	if err != nil {
		t.Fatalf("ListCards removed: %v", err)
	}
	if len(cards) != 1 || cards[0].ID != card.ID {
		t.Fatalf("expected removed card %d, got %#v", card.ID, cards)
	}

	if err := svc.RestoreCard(ctx, card.ID); err != nil {
		t.Fatalf("RestoreCard: %v", err)
	}

	cards, err = svc.ListCards(ctx, deckID, "active")
	if err != nil {
		t.Fatalf("ListCards active after restore: %v", err)
	}
	if len(cards) != 1 || cards[0].ID != card.ID {
		t.Fatalf("expected active card %d after restore, got %#v", card.ID, cards)
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
