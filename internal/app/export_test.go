package app

import (
	"context"
	"strings"
	"testing"
	"word-learning/internal/storage/sqlite"
)

func TestExportDeckForUser_DeckNotFound(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()

	deck, err := store.CreateDeckForOwner(ctx, 100, "MyDeck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}

	_, err = svc.ExportDeckForUser(ctx, 999, deck.ID)
	if err == nil {
		t.Fatal("expected error for non-owned deck")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found: %v", err)
	}
}

func TestExportDeckForUser_EmptyDeck(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()

	deck, err := store.CreateDeckForOwner(ctx, 100, "Empty", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}

	data, err := svc.ExportDeckForUser(ctx, 100, deck.ID)
	if err != nil {
		t.Fatalf("ExportDeckForUser: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty JSON")
	}
	if !strings.Contains(string(data), `"cards": []`) {
		t.Errorf("expected empty cards array: %s", string(data))
	}
	if !strings.Contains(string(data), `"name": "Empty"`) {
		t.Errorf("expected deck name: %s", string(data))
	}
}

func TestCreateDeckFromExportForUser_Success(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()

	deck1, err := store.CreateDeckForOwner(ctx, 100, "Original", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}
	_, err = store.CreateCard(ctx, sqlite.CardCreateParams{
		DeckID: deck1.ID, Front: "hello", Back: "привет", Pronunciation: "", Example: "", Conjugation: "",
	})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	_, err = store.CreateCard(ctx, sqlite.CardCreateParams{
		DeckID: deck1.ID, Front: "world", Back: "мир", Pronunciation: "", Example: "", Conjugation: "",
	})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	data, err := svc.ExportDeckForUser(ctx, 100, deck1.ID)
	if err != nil {
		t.Fatalf("ExportDeckForUser: %v", err)
	}

	deck2, count, err := svc.CreateDeckFromExportForUser(ctx, 200, data)
	if err != nil {
		t.Fatalf("CreateDeckFromExportForUser: %v", err)
	}
	if count != 2 {
		t.Errorf("imported count: got %d, want 2", count)
	}
	if deck2.TelegramUserID != 200 {
		t.Errorf("deck owner: got %d, want 200", deck2.TelegramUserID)
	}
	if deck2.Name != "Original" {
		t.Errorf("deck name: got %q, want Original", deck2.Name)
	}

	cards, err := store.ListCardsForOwner(ctx, deck2.ID, 200, nil)
	if err != nil {
		t.Fatalf("ListCardsForOwner: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(cards))
	}
}

func TestImportCardsToDeckForUser_Success(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()

	deck, err := store.CreateDeckForOwner(ctx, 100, "Target", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}

	data := []byte(`{"version":1,"deck":{"name":"Exported","language_from":"EN","language_to":"RU"},"cards":[{"front":"x","back":"икс","pronunciation":"","example":"","conjugation":""},{"front":"y","back":"игрек","pronunciation":"","example":"","conjugation":""}]}`)

	report, err := svc.ImportCardsToDeckForUser(ctx, 100, deck.ID, data)
	if err != nil {
		t.Fatalf("ImportCardsToDeckForUser: %v", err)
	}
	if report.Created != 2 {
		t.Errorf("created: got %d, want 2", report.Created)
	}

	cards, err := store.ListCards(ctx, deck.ID, nil)
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(cards))
	}
	if cards[0].Front != "x" || cards[1].Front != "y" {
		t.Errorf("cards: got %q, %q", cards[0].Front, cards[1].Front)
	}
}

func TestImportCardsToDeckForUser_LanguageMismatch(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()

	deck, err := store.CreateDeckForOwner(ctx, 100, "EN-RU", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}

	// Export has EN->ES, deck has EN->RU
	data := []byte(`{"version":1,"deck":{"name":"X","language_from":"EN","language_to":"ES"},"cards":[{"front":"a","back":"b","pronunciation":"","example":"","conjugation":""}]}`)

	_, err = svc.ImportCardsToDeckForUser(ctx, 100, deck.ID, data)
	if err == nil {
		t.Fatal("expected error for language mismatch")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Errorf("error should mention mismatch: %v", err)
	}
}
