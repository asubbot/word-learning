package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"word-learning/internal/domain"
	"word-learning/internal/storage/sqlite"
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

func TestListDecksAll(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()

	if _, err := svc.CreateDeck(ctx, "CLI Deck", "en", "ru"); err != nil {
		t.Fatalf("CreateDeck: %v", err)
	}
	if _, err := store.CreateDeckForOwner(ctx, 101, "Bot Deck", "EN", "RU"); err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}

	all, err := svc.ListDecksAll(ctx)
	if err != nil {
		t.Fatalf("ListDecksAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 decks, got %d", len(all))
	}
}

func TestGetDeckByID(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()

	_, _ = svc.CreateDeck(ctx, "CLI", "en", "ru")
	botDeck, err := store.CreateDeckForOwner(ctx, 101, "Bot", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}

	got, err := svc.GetDeckByID(ctx, botDeck.ID)
	if err != nil {
		t.Fatalf("GetDeckByID: %v", err)
	}
	if got == nil || got.ID != botDeck.ID || got.TelegramUserID != 101 {
		t.Fatalf("unexpected deck: %#v", got)
	}

	missing, err := svc.GetDeckByID(ctx, 99999)
	if err != nil {
		t.Fatalf("GetDeckByID missing: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing deck, got %#v", missing)
	}
}

func TestDeckUseAndCurrentForUser(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.DeckUseForUser(ctx, 101, "basic"); err == nil {
		t.Fatal("expected not found for missing deck")
	}

	if _, err := svc.CreateDeckForUser(ctx, 101, "English Basics", "EN", "RU"); err != nil {
		t.Fatalf("CreateDeckForUser #1: %v", err)
	}
	if _, err := svc.CreateDeckForUser(ctx, 101, "English Advanced", "EN", "DE"); err != nil {
		t.Fatalf("CreateDeckForUser #2: %v", err)
	}

	_, err := svc.DeckUseForUser(ctx, 101, "English")
	if !errors.Is(err, ErrDeckNameAmbiguous) {
		t.Fatalf("expected ErrDeckNameAmbiguous, got %v", err)
	}

	useResult, err := svc.DeckUseForUser(ctx, 101, "English Basics")
	if err != nil {
		t.Fatalf("DeckUseForUser exact: %v", err)
	}
	if useResult.Deck == nil || useResult.Deck.Name != "English Basics" {
		t.Fatalf("unexpected selected deck: %#v", useResult)
	}

	current, err := svc.DeckCurrentForUser(ctx, 101)
	if err != nil {
		t.Fatalf("DeckCurrentForUser: %v", err)
	}
	if current == nil || current.Name != "English Basics" {
		t.Fatalf("unexpected current deck: %#v", current)
	}
}

func TestActiveDeckCardFlow(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()
	expectActiveDeckNotSet(t, func() error {
		_, err := svc.AddCardForActiveDeckForUser(ctx, 101, "word", "перевод", "", "", "")
		return err
	})

	deck := mustCreateDeckForUser(t, svc, ctx, 101, "Portuguese Verbs", "PT", "RU")
	mustDeckUseForUser(t, svc, ctx, 101, deck.Name)

	card := mustAddCardForActiveDeck(t, svc, ctx, 101, deck.ID, "ir", "идти", "/iɾ/", "Eu vou agora.", "vou / vais / vai / vamos / vão")
	assertActiveDeckListsOneCard(t, svc, ctx, 101, card)
	assertNextCardWithStatsMatches(t, svc, ctx, 101, card.ID, 1)
}

func expectActiveDeckNotSet(t *testing.T, fn func() error) {
	t.Helper()
	if err := fn(); !errors.Is(err, ErrActiveDeckNotSet) {
		t.Fatalf("expected ErrActiveDeckNotSet, got %v", err)
	}
}

func mustCreateDeckForUser(t *testing.T, svc *Service, ctx context.Context, userID int64, name, from, to string) domain.Deck {
	t.Helper()
	deck, err := svc.CreateDeckForUser(ctx, userID, name, from, to)
	if err != nil {
		t.Fatalf("CreateDeckForUser: %v", err)
	}
	return deck
}

func mustDeckUseForUser(t *testing.T, svc *Service, ctx context.Context, userID int64, name string) {
	t.Helper()
	if _, err := svc.DeckUseForUser(ctx, userID, name); err != nil {
		t.Fatalf("DeckUseForUser: %v", err)
	}
}

func mustAddCardForActiveDeck(t *testing.T, svc *Service, ctx context.Context, userID, deckID int64, front, back, pron, ex, conj string) domain.Card {
	t.Helper()
	card, err := svc.AddCardForActiveDeckForUser(ctx, userID, front, back, pron, ex, conj)
	if err != nil {
		t.Fatalf("AddCardForActiveDeckForUser: %v", err)
	}
	if card.DeckID != deckID {
		t.Fatalf("expected card deck %d, got %d", deckID, card.DeckID)
	}
	return card
}

func assertActiveDeckListsOneCard(t *testing.T, svc *Service, ctx context.Context, userID int64, card domain.Card) {
	t.Helper()
	cards, err := svc.ListCardsForActiveDeckForUser(ctx, userID, "active")
	if err != nil {
		t.Fatalf("ListCardsForActiveDeckForUser: %v", err)
	}
	if len(cards) != 1 || cards[0].ID != card.ID {
		t.Fatalf("unexpected cards for active deck: %#v", cards)
	}
}

func assertNextCardWithStatsMatches(t *testing.T, svc *Service, ctx context.Context, userID int64, cardID int64, total int64) {
	t.Helper()
	next, stats, err := svc.NextCardWithStatsForActiveDeckForUser(ctx, userID)
	if err != nil {
		t.Fatalf("NextCardWithStatsForActiveDeckForUser: %v", err)
	}
	if next == nil || next.ID != cardID {
		t.Fatalf("unexpected next card: %#v", next)
	}
	if stats.Total != total {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

func TestAddCardToDeck_AndListCardsInDeck(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()

	botDeck, err := store.CreateDeckForOwner(ctx, 101, "Bot", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}

	card, err := svc.AddCardToDeck(ctx, botDeck.ID, "word", "перевод", "/w/", "example", "")
	if err != nil {
		t.Fatalf("AddCardToDeck: %v", err)
	}
	if card.DeckID != botDeck.ID || card.Front != "word" {
		t.Fatalf("unexpected card: %#v", card)
	}

	cards, err := svc.ListCardsInDeck(ctx, botDeck.ID, "active")
	if err != nil {
		t.Fatalf("ListCardsInDeck: %v", err)
	}
	if len(cards) != 1 || cards[0].ID != card.ID {
		t.Fatalf("expected one card, got %#v", cards)
	}
}

func TestRemoveCardByID(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()

	botDeck, err := store.CreateDeckForOwner(ctx, 101, "Bot", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}
	card, err := svc.AddCardToDeck(ctx, botDeck.ID, "word", "back", "", "", "")
	if err != nil {
		t.Fatalf("AddCardToDeck: %v", err)
	}

	if err := svc.RemoveCardByID(ctx, card.ID); err != nil {
		t.Fatalf("RemoveCardByID: %v", err)
	}

	cards, err := svc.ListCardsInDeck(ctx, botDeck.ID, "removed")
	if err != nil {
		t.Fatalf("ListCardsInDeck: %v", err)
	}
	if len(cards) != 1 || cards[0].Status != "removed" {
		t.Fatalf("expected one removed card, got %#v", cards)
	}
}

func TestRemoveCardByID_NotFound(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()

	err := svc.RemoveCardByID(ctx, 99999)
	if err == nil {
		t.Fatal("expected error for non-existent card")
	}
	if !errors.Is(err, ErrCardNotFound) {
		t.Fatalf("expected ErrCardNotFound, got %v", err)
	}
}

func TestNextCardWithStatsInDeck(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()

	botDeck, err := store.CreateDeckForOwner(ctx, 101, "Bot", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}
	card, err := svc.AddCardToDeck(ctx, botDeck.ID, "word", "back", "", "", "")
	if err != nil {
		t.Fatalf("AddCardToDeck: %v", err)
	}

	next, stats, err := svc.NextCardWithStatsInDeck(ctx, botDeck.ID)
	if err != nil {
		t.Fatalf("NextCardWithStatsInDeck: %v", err)
	}
	if next == nil || next.ID != card.ID {
		t.Fatalf("expected next card %d, got %#v", card.ID, next)
	}
	if stats.Active != 1 || stats.Total != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestRestoreCardByID(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()

	botDeck, err := store.CreateDeckForOwner(ctx, 101, "Bot", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}
	card, err := svc.AddCardToDeck(ctx, botDeck.ID, "word", "back", "", "", "")
	if err != nil {
		t.Fatalf("AddCardToDeck: %v", err)
	}
	if err := svc.RemoveCardByID(ctx, card.ID); err != nil {
		t.Fatalf("RemoveCardByID: %v", err)
	}

	if err := svc.RestoreCardByID(ctx, card.ID); err != nil {
		t.Fatalf("RestoreCardByID: %v", err)
	}

	cards, err := svc.ListCardsInDeck(ctx, botDeck.ID, "active")
	if err != nil {
		t.Fatalf("ListCardsInDeck: %v", err)
	}
	if len(cards) != 1 || cards[0].Status != "active" {
		t.Fatalf("expected one active card after restore, got %#v", cards)
	}
}

func TestRememberCardByID(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()

	botDeck, err := store.CreateDeckForOwner(ctx, 101, "Bot", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}
	card, err := svc.AddCardToDeck(ctx, botDeck.ID, "word", "back", "", "", "")
	if err != nil {
		t.Fatalf("AddCardToDeck: %v", err)
	}

	if err := svc.RememberCardByID(ctx, card.ID); err != nil {
		t.Fatalf("RememberCardByID: %v", err)
	}

	next, _, err := svc.NextCardWithStatsInDeck(ctx, botDeck.ID)
	if err != nil {
		t.Fatalf("NextCardWithStatsInDeck: %v", err)
	}
	if next != nil {
		t.Fatalf("expected no due card after remember (interval in future), got %#v", next)
	}
}

func TestDontRememberCardByID(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()

	botDeck, err := store.CreateDeckForOwner(ctx, 101, "Bot", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}
	card, err := svc.AddCardToDeck(ctx, botDeck.ID, "word", "back", "", "", "")
	if err != nil {
		t.Fatalf("AddCardToDeck: %v", err)
	}

	if err := svc.DontRememberCardByID(ctx, card.ID); err != nil {
		t.Fatalf("DontRememberCardByID: %v", err)
	}

	// Card should still be in deck; status unchanged (scheduler updated)
	cards, err := svc.ListCardsInDeck(ctx, botDeck.ID, "active")
	if err != nil {
		t.Fatalf("ListCardsInDeck: %v", err)
	}
	if len(cards) != 1 || cards[0].ID != card.ID {
		t.Fatalf("expected one active card after dont-remember, got %#v", cards)
	}
}

func TestServiceCardLifecycle(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()
	deckID := mustCreateDeck(t, svc)

	card := mustAddLifecycleCard(t, svc, ctx, deckID)
	assertNextCardID(t, svc, ctx, deckID, card.ID, "active")

	mustRememberCard(t, svc, ctx, card.ID)
	assertNoNextCard(t, svc, ctx, deckID, "after remember")
	assertRememberState(t, store, ctx, card.ID)
	assertDeckStats(t, svc, ctx, deckID, 0, 1, 1, "after remember")

	mustDontRememberCard(t, svc, ctx, card.ID)
	assertNoNextCard(t, svc, ctx, deckID, "after dont-remember")
	assertDontRememberState(t, store, ctx, card.ID)

	mustRemoveCard(t, svc, ctx, card.ID)
	assertRemovedCardListed(t, svc, ctx, deckID, card.ID)
	assertDeckStats(t, svc, ctx, deckID, 0, 0, 0, "after remove")

	mustRestoreCard(t, svc, ctx, card.ID)
	assertNextCardID(t, svc, ctx, deckID, card.ID, "after restore")
}

func TestServiceCardValidationAndNotFound(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()
	deckID := mustCreateDeck(t, svc)

	t.Run("add_validation", func(t *testing.T) {
		expectErr(t, "invalid deck id", func() error { _, err := svc.AddCard(ctx, 0, "front", "back", "/f/", "desc", ""); return err })
		expectErr(t, "empty front", func() error { _, err := svc.AddCard(ctx, deckID, " ", "back", "/f/", "desc", ""); return err })
		expectErr(t, "empty back", func() error { _, err := svc.AddCard(ctx, deckID, "front", " ", "/f/", "desc", ""); return err })
		expectErr(t, "unknown deck", func() error { _, err := svc.AddCard(ctx, 999, "front", "back", "/f/", "desc", ""); return err })
	})
	seedDuplicate(t, svc, ctx, deckID)
	expectErrCardAlreadyExists(t, func() error { _, err := svc.AddCard(ctx, deckID, "  DuPlicate  ", "two", "", "", ""); return err })

	t.Run("list_next_validation", func(t *testing.T) {
		expectErr(t, "invalid status", func() error { _, err := svc.ListCards(ctx, deckID, "wrong"); return err })
		expectErr(t, "invalid deck id NextCard", func() error { _, err := svc.NextCard(ctx, 0); return err })
	})

	t.Run("not_found_operations", func(t *testing.T) {
		expectErrCardNotFound(t, func() error { return svc.RemoveCard(ctx, 9999) })
		expectErrCardNotFound(t, func() error { return svc.RestoreCard(ctx, 9999) })
		expectErrCardNotFound(t, func() error { return svc.RememberCard(ctx, 9999) })
		expectErrCardNotFound(t, func() error { return svc.DontRememberCard(ctx, 9999) })
	})
}

func expectErr(t *testing.T, msg string, fn func() error) {
	t.Helper()
	if err := fn(); err == nil {
		t.Fatalf("expected error (%s)", msg)
	}
}

func seedDuplicate(t *testing.T, svc *Service, ctx context.Context, deckID int64) {
	t.Helper()
	if _, err := svc.AddCard(ctx, deckID, "duplicate", "one", "", "", ""); err != nil {
		t.Fatalf("unexpected error on first duplicate seed: %v", err)
	}
}

func expectErrCardAlreadyExists(t *testing.T, fn func() error) {
	t.Helper()
	if err := fn(); !errors.Is(err, ErrCardAlreadyExists) {
		t.Fatalf("expected ErrCardAlreadyExists, got %v", err)
	}
}

func expectErrCardNotFound(t *testing.T, fn func() error) {
	t.Helper()
	if err := fn(); !errors.Is(err, ErrCardNotFound) {
		t.Fatalf("expected ErrCardNotFound, got %v", err)
	}
}

func TestGetCardByIDForUser(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()
	deckID := mustCreateDeck(t, svc)
	const ownerID int64 = 0

	card, err := svc.AddCardForUser(ctx, ownerID, deckID, "front", "back", "", "", "")
	if err != nil {
		t.Fatalf("AddCardForUser: %v", err)
	}

	got, err := svc.GetCardByIDForUser(ctx, ownerID, card.ID)
	if err != nil {
		t.Fatalf("GetCardByIDForUser: %v", err)
	}
	if got == nil || got.ID != card.ID || got.Front != "front" || got.Back != "back" {
		t.Fatalf("unexpected card: %#v", got)
	}

	expectErrCardNotFound(t, func() error { _, err := svc.GetCardByIDForUser(ctx, ownerID, 99999); return err })
	expectValidationPositive(t, func() error { _, err := svc.GetCardByIDForUser(ctx, ownerID, 0); return err })
	expectValidationPositive(t, func() error { _, err := svc.GetCardByIDForUser(ctx, ownerID, -1); return err })
}

func expectValidationPositive(t *testing.T, fn func() error) {
	t.Helper()
	err := fn()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "positive") {
		t.Fatalf("expected validation error (positive), got %v", err)
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

func TestServiceOwnerIsolation(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()

	deck, err := svc.CreateDeckForUser(ctx, 101, "Shared", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForUser: %v", err)
	}
	card, err := svc.AddCardForUser(ctx, 101, deck.ID, "banished", "exiled", "", "", "")
	if err != nil {
		t.Fatalf("AddCardForUser owner 101: %v", err)
	}

	if _, err := svc.AddCardForUser(ctx, 202, deck.ID, "intrude", "fail", "", "", ""); err == nil {
		t.Fatal("expected add card error for foreign owner")
	}

	if _, _, err := svc.NextCardWithStatsForUser(ctx, 202, deck.ID); err == nil {
		t.Fatal("expected next card error for foreign owner")
	}

	if err := svc.RememberCardForUser(ctx, 202, card.ID); !errors.Is(err, ErrCardNotFound) {
		t.Fatalf("expected ErrCardNotFound for foreign remember, got %v", err)
	}
	if err := svc.RemoveCardForUser(ctx, 202, card.ID); !errors.Is(err, ErrCardNotFound) {
		t.Fatalf("expected ErrCardNotFound for foreign remove, got %v", err)
	}
}

func TestSharedEntryAndIndependentProgress(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()

	deck1 := mustCreateUserDeck(t, svc, ctx, 101, "U1", "EN", "RU")
	deck2 := mustCreateUserDeck(t, svc, ctx, 202, "U2", "EN", "RU")

	c1 := mustAddUserCard(t, svc, ctx, 101, deck1.ID, "banished", "old", "", "", "")
	c2 := mustAddUserCard(t, svc, ctx, 202, deck2.ID, "banished", "new", "", "updated", "")
	assertSharedEntry(t, c1.EntryID, c2.EntryID)
	assertSharedLatestContent(t, store, ctx, c1.ID, "new", "updated")
	assertIndependentProgress(t, svc, store, ctx, 101, c1.ID, c2.ID)
}

func mustAddLifecycleCard(t *testing.T, svc *Service, ctx context.Context, deckID int64) domain.Card {
	t.Helper()
	card, err := svc.AddCard(ctx, deckID, " banished ", " exiled ", " /banished/ ", "  sample ", "")
	if err != nil {
		t.Fatalf("AddCard: %v", err)
	}
	if card.Front != "banished" || card.Back != "exiled" || card.Pronunciation != "/banished/" || card.Example != "sample" {
		t.Fatalf("unexpected trimmed values: %#v", card)
	}
	return card
}

func assertNextCardID(t *testing.T, svc *Service, ctx context.Context, deckID, wantID int64, label string) {
	t.Helper()
	next, err := svc.NextCard(ctx, deckID)
	if err != nil {
		t.Fatalf("NextCard %s: %v", label, err)
	}
	if next == nil || next.ID != wantID {
		t.Fatalf("expected next card %d (%s), got %#v", wantID, label, next)
	}
}

func assertNoNextCard(t *testing.T, svc *Service, ctx context.Context, deckID int64, label string) {
	t.Helper()
	next, err := svc.NextCard(ctx, deckID)
	if err != nil {
		t.Fatalf("NextCard %s: %v", label, err)
	}
	if next != nil {
		t.Fatalf("expected no due card %s, got %#v", label, next)
	}
}

func mustRememberCard(t *testing.T, svc *Service, ctx context.Context, cardID int64) {
	t.Helper()
	if err := svc.RememberCard(ctx, cardID); err != nil {
		t.Fatalf("RememberCard: %v", err)
	}
}

func assertRememberState(t *testing.T, store *sqlite.Store, ctx context.Context, cardID int64) {
	t.Helper()
	stored, err := store.GetCardByID(ctx, cardID)
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
}

func assertDeckStats(t *testing.T, svc *Service, ctx context.Context, deckID, active, postponed, total int64, label string) {
	t.Helper()
	_, stats, err := svc.NextCardWithStats(ctx, deckID)
	if err != nil {
		t.Fatalf("NextCardWithStats %s: %v", label, err)
	}
	if stats.Active != active || stats.Postponed != postponed || stats.Total != total {
		t.Fatalf("unexpected stats %s: %#v", label, stats)
	}
}

func mustDontRememberCard(t *testing.T, svc *Service, ctx context.Context, cardID int64) {
	t.Helper()
	if err := svc.DontRememberCard(ctx, cardID); err != nil {
		t.Fatalf("DontRememberCard: %v", err)
	}
}

func assertDontRememberState(t *testing.T, store *sqlite.Store, ctx context.Context, cardID int64) {
	t.Helper()
	stored, err := store.GetCardByID(ctx, cardID)
	if err != nil {
		t.Fatalf("GetCardByID after dont-remember: %v", err)
	}
	if stored == nil {
		t.Fatal("expected card after dont-remember")
	}
	if stored.Lapses != 1 {
		t.Fatalf("expected lapses=1, got %d", stored.Lapses)
	}
	if stored.IntervalSec != 600 {
		t.Fatalf("expected interval_sec=600, got %d", stored.IntervalSec)
	}
}

func mustRemoveCard(t *testing.T, svc *Service, ctx context.Context, cardID int64) {
	t.Helper()
	if err := svc.RemoveCard(ctx, cardID); err != nil {
		t.Fatalf("RemoveCard: %v", err)
	}
}

func assertRemovedCardListed(t *testing.T, svc *Service, ctx context.Context, deckID, cardID int64) {
	t.Helper()
	cards, err := svc.ListCards(ctx, deckID, "removed")
	if err != nil {
		t.Fatalf("ListCards removed: %v", err)
	}
	if len(cards) != 1 || cards[0].ID != cardID {
		t.Fatalf("expected removed card %d, got %#v", cardID, cards)
	}
}

func mustRestoreCard(t *testing.T, svc *Service, ctx context.Context, cardID int64) {
	t.Helper()
	if err := svc.RestoreCard(ctx, cardID); err != nil {
		t.Fatalf("RestoreCard: %v", err)
	}
}

func mustCreateUserDeck(t *testing.T, svc *Service, ctx context.Context, userID int64, name, from, to string) domain.Deck {
	t.Helper()
	deck, err := svc.CreateDeckForUser(ctx, userID, name, from, to)
	if err != nil {
		t.Fatalf("CreateDeckForUser %s: %v", name, err)
	}
	return deck
}

func mustAddUserCard(t *testing.T, svc *Service, ctx context.Context, userID, deckID int64, front, back, pron, example, conjugation string) domain.Card {
	t.Helper()
	card, err := svc.AddCardForUser(ctx, userID, deckID, front, back, pron, example, conjugation)
	if err != nil {
		t.Fatalf("AddCardForUser user=%d: %v", userID, err)
	}
	return card
}

func assertSharedEntry(t *testing.T, leftEntryID, rightEntryID int64) {
	t.Helper()
	if leftEntryID == 0 || rightEntryID == 0 || leftEntryID != rightEntryID {
		t.Fatalf("expected shared entry id, got left=%d right=%d", leftEntryID, rightEntryID)
	}
}

func assertSharedLatestContent(t *testing.T, store *sqlite.Store, ctx context.Context, cardID int64, wantBack, wantExample string) {
	t.Helper()
	reloaded, err := store.GetCardByID(ctx, cardID)
	if err != nil {
		t.Fatalf("GetCardByID shared content: %v", err)
	}
	if reloaded == nil || reloaded.Back != wantBack || reloaded.Example != wantExample {
		t.Fatalf("expected shared latest content back=%q example=%q, got %#v", wantBack, wantExample, reloaded)
	}
}

func assertIndependentProgress(t *testing.T, svc *Service, store *sqlite.Store, ctx context.Context, userID, leftCardID, rightCardID int64) {
	t.Helper()
	if err := svc.RememberCardForUser(ctx, userID, leftCardID); err != nil {
		t.Fatalf("RememberCardForUser user=%d: %v", userID, err)
	}
	left, err := store.GetCardByID(ctx, leftCardID)
	if err != nil {
		t.Fatalf("GetCardByID left: %v", err)
	}
	right, err := store.GetCardByID(ctx, rightCardID)
	if err != nil {
		t.Fatalf("GetCardByID right: %v", err)
	}
	if left == nil || right == nil {
		t.Fatalf("expected both cards, left=%#v right=%#v", left, right)
	}
	if left.IntervalSec == right.IntervalSec && left.NextDueAt.Equal(right.NextDueAt) {
		t.Fatalf("expected independent progress, left=%#v right=%#v", left, right)
	}
}
