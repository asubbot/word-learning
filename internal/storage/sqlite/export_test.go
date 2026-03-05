package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"word-learning/internal/domain"
	"word-learning/internal/export"
)

func TestExportDeckForOwner_NotFound(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "export.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if err := store.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	deck := mustCreateDeckForOwnerStoreTest(t, store, ctx, 100, "MyDeck", "EN", "RU")
	_, _, err = store.ExportDeckForOwner(ctx, deck.ID, 999)
	if err == nil {
		t.Fatal("expected error when deck not owned by user")
	}
}

func TestExportDeckForOwner_Success(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "export.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if err := store.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	deck := mustCreateDeckForOwnerStoreTest(t, store, ctx, 100, "English Basics", "EN", "RU")
	mustCreateStoreCard(t, store, ctx, deck.ID, "banished", "изгнанный", "/banished/", "He was banished.", "")
	mustCreateStoreCard(t, store, ctx, deck.ID, "come up with", "придумать", "", "", "")

	exportedDeck, cards, err := store.ExportDeckForOwner(ctx, deck.ID, 100)
	if err != nil {
		t.Fatalf("ExportDeckForOwner: %v", err)
	}
	if exportedDeck.Name != "English Basics" || exportedDeck.LanguageFrom != "EN" || exportedDeck.LanguageTo != "RU" {
		t.Errorf("deck meta: got %+v", exportedDeck)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(cards))
	}
	if cards[0].Front != "banished" || cards[0].Back != "изгнанный" {
		t.Errorf("card 0: front=%q back=%q", cards[0].Front, cards[0].Back)
	}
	if cards[1].Front != "come up with" || cards[1].Back != "придумать" {
		t.Errorf("card 1: front=%q back=%q", cards[1].Front, cards[1].Back)
	}
}

func TestExportDeckForOwner_ExcludesRemoved(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "export.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if err := store.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	deck := mustCreateDeckForOwnerStoreTest(t, store, ctx, 100, "Deck", "EN", "RU")
	mustCreateStoreCard(t, store, ctx, deck.ID, "a", "x", "", "", "")
	card2 := mustCreateStoreCard(t, store, ctx, deck.ID, "b", "y", "", "", "")
	mustSetStoreStatus(t, store, ctx, card2.ID, domain.CardStatusRemoved)

	_, cards, err := store.ExportDeckForOwner(ctx, deck.ID, 100)
	if err != nil {
		t.Fatalf("ExportDeckForOwner: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 active card (removed excluded), got %d", len(cards))
	}
	if cards[0].Front != "a" {
		t.Errorf("expected card 'a', got %q", cards[0].Front)
	}
}

func assertCardFields(t *testing.T, cards []domain.Card, want []struct {
	Front, Back, Pronunciation, Example, Conjugation string
},
) {
	t.Helper()
	if len(cards) != len(want) {
		t.Fatalf("cards: got %d, want %d", len(cards), len(want))
	}
	for i, w := range want {
		c := cards[i]
		if c.Front != w.Front || c.Back != w.Back || c.Pronunciation != w.Pronunciation ||
			c.Example != w.Example || c.Conjugation != w.Conjugation {
			t.Errorf("card %d: got Front=%q Back=%q Pronunciation=%q Example=%q Conjugation=%q",
				i, c.Front, c.Back, c.Pronunciation, c.Example, c.Conjugation)
		}
	}
}

func TestImportDeckForOwner_Success(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "import.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if err := store.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	contents := []export.CardContent{
		{Front: "one", Back: "один", Pronunciation: "/wʌn/", Example: "One day.", Conjugation: ""},
		{Front: "two", Back: "два", Pronunciation: "", Example: "", Conjugation: ""},
		{Front: "three", Back: "три", Pronunciation: "/θriː/", Example: "Three times.", Conjugation: "verb"},
	}

	deck, count, err := store.ImportDeckForOwner(ctx, 200, "Imported", "EN", "RU", contents)
	if err != nil {
		t.Fatalf("ImportDeckForOwner: %v", err)
	}
	if count != 3 {
		t.Errorf("imported count: got %d, want 3", count)
	}
	if deck.Name != "Imported" || deck.TelegramUserID != 200 {
		t.Errorf("deck: got %+v", deck)
	}

	cards, err := store.ListCards(ctx, deck.ID, nil)
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	assertCardFields(t, cards, []struct {
		Front, Back, Pronunciation, Example, Conjugation string
	}{
		{"one", "один", "/wʌn/", "One day.", ""},
		{"two", "два", "", "", ""},
		{"three", "три", "/θriː/", "Three times.", "verb"},
	})
}

func TestImportDeckForOwner_ReusesEntries(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "import.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if err := store.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	contents := []export.CardContent{
		{Front: "shared", Back: "общий", Pronunciation: "", Example: "", Conjugation: ""},
	}

	deck1, _, err := store.ImportDeckForOwner(ctx, 301, "Deck1", "EN", "RU", contents)
	if err != nil {
		t.Fatalf("ImportDeckForOwner: %v", err)
	}
	deck2, _, err := store.ImportDeckForOwner(ctx, 302, "Deck2", "EN", "RU", contents)
	if err != nil {
		t.Fatalf("ImportDeckForOwner deck2: %v", err)
	}

	cards1, _ := store.ListCards(ctx, deck1.ID, nil)
	cards2, _ := store.ListCards(ctx, deck2.ID, nil)
	if len(cards1) != 1 || len(cards2) != 1 {
		t.Fatalf("expected 1 card each, got %d and %d", len(cards1), len(cards2))
	}
	if cards1[0].EntryID != cards2[0].EntryID {
		t.Errorf("expected shared entry: c1.entry_id=%d c2.entry_id=%d", cards1[0].EntryID, cards2[0].EntryID)
	}
}

func TestInsertCardsToDeckForOwner_Success(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "add-cards.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if err := store.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	deck := mustCreateDeckForOwnerStoreTest(t, store, ctx, 100, "Target", "EN", "RU")
	contents := []export.CardContent{
		{Front: "new1", Back: "новый1", Pronunciation: "", Example: "", Conjugation: ""},
		{Front: "new2", Back: "новый2", Pronunciation: "/n/", Example: "Ex.", Conjugation: ""},
	}

	count, err := store.InsertCardsToDeckForOwner(ctx, deck.ID, 100, contents)
	if err != nil {
		t.Fatalf("InsertCardsToDeckForOwner: %v", err)
	}
	if count != 2 {
		t.Errorf("count: got %d, want 2", count)
	}

	cards, err := store.ListCards(ctx, deck.ID, nil)
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(cards))
	}
	if cards[0].Front != "new1" || cards[1].Front != "new2" {
		t.Errorf("cards: got %q, %q", cards[0].Front, cards[1].Front)
	}
}

func TestInsertCardsToDeckForOwner_DeckNotFound(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "add-cards.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	if err := store.InitSchema(ctx); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	_, err = store.InsertCardsToDeckForOwner(ctx, 999, 100, []export.CardContent{{Front: "a", Back: "b"}})
	if err == nil {
		t.Fatal("expected error for non-existent deck")
	}
}
