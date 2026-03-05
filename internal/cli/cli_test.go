package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"word-learning/internal/app"
	"word-learning/internal/domain"
	"word-learning/internal/storage/sqlite"
)

func newTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	return store
}

func TestRunDeckCreate(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	deck, err := runDeckCreate(ctx, store, "en", "ru", "My Deck", &out)
	if err != nil {
		t.Fatalf("runDeckCreate: %v", err)
	}
	if deck.ID <= 0 || deck.Name != "My Deck" || deck.LanguageFrom != "EN" || deck.LanguageTo != "RU" {
		t.Errorf("unexpected deck: id=%d name=%q from=%s to=%s", deck.ID, deck.Name, deck.LanguageFrom, deck.LanguageTo)
	}
	if g := out.String(); !strings.Contains(g, "Deck created:") || !strings.Contains(g, "My Deck") {
		t.Errorf("output missing expected text: %q", g)
	}
}

func TestRunDeckList(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	decks, err := runDeckList(ctx, store, &out)
	if err != nil {
		t.Fatalf("runDeckList: %v", err)
	}
	if len(decks) != 0 {
		t.Errorf("expected 0 decks, got %d", len(decks))
	}
	if g := out.String(); !strings.Contains(g, "No decks yet") {
		t.Errorf("output expected 'No decks yet', got %q", g)
	}

	_, _ = runDeckCreate(ctx, store, "en", "ru", "A", nil)
	_, _ = runDeckCreate(ctx, store, "pt", "ru", "B", nil)
	out.Reset()
	decks, err = runDeckList(ctx, store, &out)
	if err != nil {
		t.Fatalf("runDeckList: %v", err)
	}
	if len(decks) != 2 {
		t.Errorf("expected 2 decks, got %d", len(decks))
	}
	if g := out.String(); !strings.Contains(g, "A") || !strings.Contains(g, "B") {
		t.Errorf("output expected deck names: %q", g)
	}
}

func TestRunDeckUseAndCurrent(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "Active One", nil)

	deck, err := runDeckUse(ctx, store, "Active One", &out)
	if err != nil {
		t.Fatalf("runDeckUse: %v", err)
	}
	if deck == nil || deck.Name != "Active One" {
		t.Errorf("runDeckUse: expected deck Active One, got %v", deck)
	}

	out.Reset()
	current, err := runDeckCurrent(ctx, store, &out)
	if err != nil {
		t.Fatalf("runDeckCurrent: %v", err)
	}
	if current == nil || current.Name != "Active One" {
		t.Errorf("runDeckCurrent: expected Active One, got %v", current)
	}
}

func TestRunDeckCurrent_NoActiveDeck(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	_, err := runDeckCurrent(ctx, store, nil)
	if err == nil {
		t.Fatal("expected error when no active deck")
	}
	if !strings.Contains(err.Error(), "active deck is not set") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCardAdd_NoActiveDeck(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	_, err := runCardAdd(ctx, store, "hello", "привет", "", "", "", nil)
	if err == nil {
		t.Fatal("expected error when no active deck")
	}
	if !strings.Contains(err.Error(), "active deck is not set") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCardAddAndList(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "D", nil)
	_, _ = runDeckUse(ctx, store, "D", nil)

	card, err := runCardAdd(ctx, store, "cat", "кошка", "/kæt/", "a cat", "", &out)
	if err != nil {
		t.Fatalf("runCardAdd: %v", err)
	}
	if card.ID <= 0 || card.Front != "cat" || card.Back != "кошка" {
		t.Errorf("unexpected card: %+v", card)
	}

	out.Reset()
	cards, err := runCardList(ctx, store, "", &out)
	if err != nil {
		t.Fatalf("runCardList: %v", err)
	}
	if len(cards) != 1 || cards[0].Front != "cat" {
		t.Errorf("runCardList: expected one card 'cat', got %d: %+v", len(cards), cards)
	}
}

func TestRunCardGet_NoCards(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "D", nil)
	_, _ = runDeckUse(ctx, store, "D", nil)

	card, stats, err := runCardGet(ctx, store, &out)
	if err != nil {
		t.Fatalf("runCardGet: %v", err)
	}
	if card != nil {
		t.Errorf("expected nil card when no cards, got %v", card)
	}
	if stats != nil {
		t.Errorf("expected nil stats when no card, got %v", stats)
	}
	if g := out.String(); !strings.Contains(g, "No available cards") {
		t.Errorf("output expected 'No available cards', got %q", g)
	}
}

func TestRunCardGet_ReturnsCard(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "D", nil)
	_, _ = runDeckUse(ctx, store, "D", nil)
	added, _ := runCardAdd(ctx, store, "dog", "собака", "", "", "", nil)

	out.Reset()
	card, stats, err := runCardGet(ctx, store, &out)
	if err != nil {
		t.Fatalf("runCardGet: %v", err)
	}
	if card == nil {
		t.Fatal("expected a card")
	}
	if card.ID != added.ID || card.Front != "dog" {
		t.Errorf("unexpected card: id=%d front=%s", card.ID, card.Front)
	}
	if stats != nil && stats.Total != 1 {
		t.Errorf("expected stats.Total=1, got %d", stats.Total)
	}
	if g := out.String(); !strings.Contains(g, "dog") || !strings.Contains(g, "собака") {
		t.Errorf("output expected card content: %q", g)
	}
}

func TestRunCardRememberAndDontRemember(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "D", nil)
	_, _ = runDeckUse(ctx, store, "D", nil)
	added, _ := runCardAdd(ctx, store, "word", "слово", "", "", "", nil)

	err := runCardRemember(ctx, store, added.ID, &out)
	if err != nil {
		t.Fatalf("runCardRemember: %v", err)
	}
	if g := out.String(); !strings.Contains(g, "longer interval") {
		t.Errorf("output expected 'longer interval': %q", g)
	}

	out.Reset()
	err = runCardDontRemember(ctx, store, added.ID, &out)
	if err != nil {
		t.Fatalf("runCardDontRemember: %v", err)
	}
	if g := out.String(); !strings.Contains(g, "short retry") {
		t.Errorf("output expected 'short retry': %q", g)
	}
}

func TestRunCardRemember_NotFound(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	err := runCardRemember(ctx, store, 99999, nil)
	if err == nil {
		t.Fatal("expected error for non-existent card")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCardRemoveAndRestore(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "D", nil)
	_, _ = runDeckUse(ctx, store, "D", nil)
	added, _ := runCardAdd(ctx, store, "x", "икс", "", "", "", nil)

	err := runCardRemove(ctx, store, added.ID, &out)
	if err != nil {
		t.Fatalf("runCardRemove: %v", err)
	}
	cards, _ := runCardList(ctx, store, "removed", nil)
	if len(cards) != 1 {
		t.Errorf("expected 1 removed card, got %d", len(cards))
	}

	out.Reset()
	err = runCardRestore(ctx, store, added.ID, &out)
	if err != nil {
		t.Fatalf("runCardRestore: %v", err)
	}
	cards, _ = runCardList(ctx, store, "active", nil)
	if len(cards) != 1 {
		t.Errorf("expected 1 active card after restore, got %d", len(cards))
	}
}

func TestRunDeckUse_AmbiguousName(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "Deck A", nil)
	_, _ = runDeckCreate(ctx, store, "en", "ru", "Deck B", nil)

	deck, err := runDeckUse(ctx, store, "Deck", &out)
	if err == nil {
		t.Fatal("expected error for ambiguous deck name")
	}
	if deck != nil {
		t.Errorf("expected nil deck on ambiguous, got %v", deck)
	}
	if !strings.Contains(err.Error(), "ambiguous") && !strings.Contains(err.Error(), "retry") && !strings.Contains(err.Error(), "Ambiguous") {
		t.Errorf("unexpected error: %v", err)
	}
	g := out.String()
	if !strings.Contains(g, "Candidates") {
		t.Errorf("output expected 'Candidates', got %q", g)
	}
	if !strings.Contains(g, "Deck A") || !strings.Contains(g, "Deck B") {
		t.Errorf("output expected both deck names, got %q", g)
	}
}

func TestRunCardList_StatusFilter(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "D", nil)
	_, _ = runDeckUse(ctx, store, "D", nil)
	card, _ := runCardAdd(ctx, store, "x", "икс", "", "", "", nil)
	_ = runCardRemove(ctx, store, card.ID, nil)

	out.Reset()
	active, err := runCardList(ctx, store, "active", &out)
	if err != nil {
		t.Fatalf("runCardList active: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("expected 0 active cards, got %d", len(active))
	}
	if g := out.String(); !strings.Contains(g, "No cards") {
		t.Errorf("output expected 'No cards' for empty active list: %q", g)
	}

	out.Reset()
	removed, err := runCardList(ctx, store, "removed", &out)
	if err != nil {
		t.Fatalf("runCardList removed: %v", err)
	}
	if len(removed) != 1 || removed[0].ID != card.ID {
		t.Errorf("expected 1 removed card, got %d: %+v", len(removed), removed)
	}
	g := out.String()
	if !strings.Contains(g, "ID\t") || !strings.Contains(g, "FRONT") {
		t.Errorf("output expected table header: %q", g)
	}
	if !strings.Contains(g, "x") || !strings.Contains(g, "икс") {
		t.Errorf("output expected card row: %q", g)
	}
}

func TestRunCardDontRemember_NotFound(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "D", nil)
	_, _ = runDeckUse(ctx, store, "D", nil)

	err := runCardDontRemember(ctx, store, 99999, &out)
	if err == nil {
		t.Fatal("expected error for non-existent card")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCardRemove_NotFound(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "D", nil)
	_, _ = runDeckUse(ctx, store, "D", nil)

	err := runCardRemove(ctx, store, 99999, &out)
	if err == nil {
		t.Fatal("expected error for non-existent card")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCardRestore_NotFound(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "D", nil)
	_, _ = runDeckUse(ctx, store, "D", nil)

	err := runCardRestore(ctx, store, 99999, &out)
	if err == nil {
		t.Fatal("expected error for non-existent card")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunCardRemove_SuccessOutput(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "D", nil)
	_, _ = runDeckUse(ctx, store, "D", nil)
	card, _ := runCardAdd(ctx, store, "a", "б", "", "", "", nil)

	out.Reset()
	err := runCardRemove(ctx, store, card.ID, &out)
	if err != nil {
		t.Fatalf("runCardRemove: %v", err)
	}
	if g := out.String(); !strings.Contains(g, "Card removed") {
		t.Errorf("output expected 'Card removed', got %q", g)
	}
}

func TestRunCardRestore_SuccessOutput(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "D", nil)
	_, _ = runDeckUse(ctx, store, "D", nil)
	card, _ := runCardAdd(ctx, store, "a", "б", "", "", "", nil)
	_ = runCardRemove(ctx, store, card.ID, nil)

	out.Reset()
	err := runCardRestore(ctx, store, card.ID, &out)
	if err != nil {
		t.Fatalf("runCardRestore: %v", err)
	}
	if g := out.String(); !strings.Contains(g, "Card restored") {
		t.Errorf("output expected 'Card restored', got %q", g)
	}
}

func TestPrintDecksAllTo_WithTelegramOwner(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "CLI Deck", nil)
	deck, err := store.CreateDeckForOwner(ctx, 42, "Bot Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}
	if deck.TelegramUserID != 42 {
		t.Fatalf("expected telegram_user_id 42, got %d", deck.TelegramUserID)
	}

	out.Reset()
	decks, err := runDeckList(ctx, store, &out)
	if err != nil {
		t.Fatalf("runDeckList: %v", err)
	}
	if len(decks) != 2 {
		t.Fatalf("expected 2 decks, got %d", len(decks))
	}
	g := out.String()
	if !strings.Contains(g, "Telegram (") || !strings.Contains(g, "42") {
		t.Errorf("output expected 'Telegram (' and owner id, got %q", g)
	}
}

func TestPrintCardsTo_EmptyAndNonEmpty(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "D", nil)
	_, _ = runDeckUse(ctx, store, "D", nil)

	out.Reset()
	cards, err := runCardList(ctx, store, "", &out)
	if err != nil {
		t.Fatalf("runCardList: %v", err)
	}
	if len(cards) != 0 {
		t.Errorf("expected 0 cards, got %d", len(cards))
	}
	var g string
	g = out.String()
	if !strings.Contains(g, "No cards found.") {
		t.Errorf("output expected 'No cards found.', got %q", g)
	}

	_, _ = runCardAdd(ctx, store, "hello", "привет", "/h/", "", "", nil)
	out.Reset()
	cards, err = runCardList(ctx, store, "", &out)
	if err != nil {
		t.Fatalf("runCardList: %v", err)
	}
	if len(cards) != 1 {
		t.Errorf("expected 1 card, got %d", len(cards))
	}
	g = out.String()
	if !strings.Contains(g, "ID\t") || !strings.Contains(g, "FRONT") || !strings.Contains(g, "BACK") {
		t.Errorf("output expected table header: %q", g)
	}
	if !strings.Contains(g, "hello") || !strings.Contains(g, "привет") {
		t.Errorf("output expected card row: %q", g)
	}
}

func TestPrintCardDetailsTo_WithOptionalFields(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _ = runDeckCreate(ctx, store, "en", "ru", "D", nil)
	_, _ = runDeckUse(ctx, store, "D", nil)
	_, _ = runCardAdd(ctx, store, "word", "слово", "/wɜːd/", "an example sentence", "form1 / form2", nil)

	out.Reset()
	card, _, err := runCardGet(ctx, store, &out)
	if err != nil {
		t.Fatalf("runCardGet: %v", err)
	}
	if card == nil {
		t.Fatal("expected a card")
	}
	g := out.String()
	if !strings.Contains(g, "Pronunciation:") || !strings.Contains(g, "/wɜːd/") {
		t.Errorf("output expected Pronunciation: %q", g)
	}
	if !strings.Contains(g, "Example:") || !strings.Contains(g, "an example sentence") {
		t.Errorf("output expected Example: %q", g)
	}
	if !strings.Contains(g, "Conjugation:") || !strings.Contains(g, "form1") {
		t.Errorf("output expected Conjugation: %q", g)
	}
}

func TestRunCardGet_ErrActiveDeckNotSet(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_, _, err := runCardGet(ctx, store, &out)
	if err == nil {
		t.Fatal("expected error when active deck is not set")
	}
	if !strings.Contains(err.Error(), "active deck is not set") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestIntegration_DeckCreateUseAddGetRemember runs the full flow: deck create → use → card add → get → remember,
// and checks that the card appears, get returns it, and after remember the next get does not return it immediately
// (card is postponed).
func TestIntegration_DeckCreateUseAddGetRemember(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	_ = integrationCreateDeck(t, ctx, store, &out)
	integrationUseDeck(t, ctx, store, &out)
	card := integrationAddCard(t, ctx, store, &out)
	integrationGetCardExpectOne(t, ctx, store, &out, card.ID)
	integrationRememberCard(t, ctx, store, &out, card.ID)
	integrationGetCardExpectNone(t, ctx, store, &out)
}

func integrationCreateDeck(t *testing.T, ctx context.Context, store *sqlite.Store, out *bytes.Buffer) (deck domain.Deck) {
	t.Helper()
	var err error
	deck, err = runDeckCreate(ctx, store, "en", "ru", "Integration Deck", out)
	if err != nil {
		t.Fatalf("deck create: %v", err)
	}
	if deck.ID <= 0 {
		t.Fatalf("deck id not set")
	}
	return deck
}

func integrationUseDeck(t *testing.T, ctx context.Context, store *sqlite.Store, out *bytes.Buffer) {
	t.Helper()
	if _, err := runDeckUse(ctx, store, "Integration Deck", out); err != nil {
		t.Fatalf("deck use: %v", err)
	}
}

func integrationAddCard(t *testing.T, ctx context.Context, store *sqlite.Store, out *bytes.Buffer) (card domain.Card) {
	t.Helper()
	var err error
	card, err = runCardAdd(ctx, store, "test", "тест", "", "", "", out)
	if err != nil {
		t.Fatalf("card add: %v", err)
	}
	if card.ID <= 0 {
		t.Fatalf("card id not set")
	}
	return card
}

func integrationGetCardExpectOne(t *testing.T, ctx context.Context, store *sqlite.Store, out *bytes.Buffer, expectCardID int64) {
	t.Helper()
	out.Reset()
	got, stats, err := runCardGet(ctx, store, out)
	if err != nil {
		t.Fatalf("card get: %v", err)
	}
	if got == nil {
		t.Fatal("card get: expected a card")
	}
	if got.ID != expectCardID || got.Front != "test" {
		t.Errorf("card get: expected id=%d front=test, got id=%d front=%s", expectCardID, got.ID, got.Front)
	}
	if stats != nil && stats.Total != 1 {
		t.Errorf("card get: expected total=1, got %d", stats.Total)
	}
}

func integrationRememberCard(t *testing.T, ctx context.Context, store *sqlite.Store, out *bytes.Buffer, cardID int64) {
	t.Helper()
	out.Reset()
	if err := runCardRemember(ctx, store, cardID, out); err != nil {
		t.Fatalf("card remember: %v", err)
	}
}

func integrationGetCardExpectNone(t *testing.T, ctx context.Context, store *sqlite.Store, out *bytes.Buffer) {
	t.Helper()
	out.Reset()
	got, _, err := runCardGet(ctx, store, out)
	if err != nil {
		t.Fatalf("card get after remember: %v", err)
	}
	if got != nil {
		t.Errorf("after remember, next get should return no card (postponed), got card id=%d", got.ID)
	}
	if g := out.String(); !strings.Contains(g, "No available cards") {
		t.Errorf("expected 'No available cards' after remember, got: %q", g)
	}
}

func setupDeckWithCardsForExport(t *testing.T, ctx context.Context, store *sqlite.Store, cards [][2]string) string {
	t.Helper()
	_, _ = runDeckCreate(ctx, store, "en", "ru", "Test", nil)
	_, _ = runDeckUse(ctx, store, "Test", nil)
	for _, pair := range cards {
		_, _ = runCardAdd(ctx, store, pair[0], pair[1], "", "", "", nil)
	}
	return filepath.Join(t.TempDir(), "deck.json")
}

func assertDeckExportImportResult(t *testing.T, imported domain.Deck, report app.ImportReport, store *sqlite.Store, wantCards [][2]string) {
	t.Helper()
	if report.Created != len(wantCards) {
		t.Errorf("import created: got %d, want %d", report.Created, len(wantCards))
	}
	if imported.Name != "Imported" {
		t.Errorf("imported deck name: got %q, want Imported", imported.Name)
	}
	decks, _ := runDeckList(context.Background(), store, nil)
	if len(decks) != 2 {
		t.Errorf("expected 2 decks after import, got %d", len(decks))
	}
	cards, err := store.ListCards(context.Background(), imported.ID, nil)
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != len(wantCards) {
		t.Fatalf("expected %d cards in imported deck, got %d", len(wantCards), len(cards))
	}
	for i, w := range wantCards {
		if cards[i].Front != w[0] || cards[i].Back != w[1] {
			t.Errorf("card %d: got front=%q back=%q, want %q %q", i, cards[i].Front, cards[i].Back, w[0], w[1])
		}
	}
}

func TestDeckExportImport_CLI(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	var out bytes.Buffer

	exportPath := setupDeckWithCardsForExport(t, ctx, store, [][2]string{{"a", "б"}, {"b", "в"}})
	_, err := runDeckExport(ctx, store, "Test", exportPath, &out)
	if err != nil {
		t.Fatalf("runDeckExport: %v", err)
	}
	if g := out.String(); !strings.Contains(g, "Exported") || !strings.Contains(g, "Test") {
		t.Errorf("export output: %q", g)
	}

	out.Reset()
	imported, report, err := runDeckImport(ctx, store, exportPath, "", "Imported", &out)
	if err != nil {
		t.Fatalf("runDeckImport: %v", err)
	}
	if g := out.String(); !strings.Contains(g, "Import summary:") || !strings.Contains(g, "created=2") {
		t.Errorf("import output: %q", g)
	}
	assertDeckExportImportResult(t, imported, report, store, [][2]string{{"a", "б"}, {"b", "в"}})
}

func TestDeckImport_DeckAndNewConflict(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	exportPath := setupDeckWithCardsForExport(t, ctx, store, [][2]string{{"a", "б"}})
	_, _ = runDeckExport(ctx, store, "Test", exportPath, nil)

	_, _, err := runDeckImport(ctx, store, exportPath, "Test", "NewDeck", nil)
	if err == nil {
		t.Fatal("expected error when both --deck and --new are set")
	}
	if !strings.Contains(err.Error(), "cannot use both") {
		t.Errorf("error should mention conflict, got: %v", err)
	}
}
