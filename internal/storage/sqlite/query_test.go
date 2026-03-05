package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
	"word-learning/internal/domain"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "sqlite-test.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	return store
}

func TestDeckQueries(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	deck, err := store.CreateDeck(ctx, "  Deck Name  ", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeck: %v", err)
	}

	exists, err := store.DeckExists(ctx, deck.ID)
	if err != nil {
		t.Fatalf("DeckExists: %v", err)
	}
	if !exists {
		t.Fatal("expected existing deck")
	}

	exists, err = store.DeckExists(ctx, deck.ID+100)
	if err != nil {
		t.Fatalf("DeckExists missing: %v", err)
	}
	if exists {
		t.Fatal("did not expect existing deck for unknown id")
	}

	decks, err := store.ListDecks(ctx)
	if err != nil {
		t.Fatalf("ListDecks: %v", err)
	}
	if len(decks) != 1 {
		t.Fatalf("expected 1 deck, got %d", len(decks))
	}
	if decks[0].Name != "Deck Name" {
		t.Fatalf("expected trimmed deck name, got %q", decks[0].Name)
	}
}

func TestListCardsWithStatusFilter(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	deck, err := store.CreateDeck(ctx, "Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeck: %v", err)
	}

	activeCard := mustCreateCardForQueryTest(t, store, ctx, deck.ID, "a", "a", "/a/", "", "")
	postponedCard := mustCreateCardForQueryTest(t, store, ctx, deck.ID, "b", "b", "/b/", "", "")
	removedCard := mustCreateCardForQueryTest(t, store, ctx, deck.ID, "c", "c", "/c/", "", "")

	future := time.Now().UTC().Add(2 * time.Hour)
	mustUpdateCardScheduleForQueryTest(t, store, ctx, postponedCard.ID, future, 600, 2.3, 1, "postponed")
	mustSetCardStatusForQueryTest(t, store, ctx, removedCard.ID, domain.CardStatusRemoved)
	assertCardCountForDeck(t, store, ctx, deck.ID, nil, 3, "all")

	activeStatus := domain.CardStatusActive
	activeCards := mustListCardsForQueryTest(t, store, ctx, deck.ID, &activeStatus, "active")
	if len(activeCards) != 2 {
		t.Fatalf("expected 2 active cards, got %#v", activeCards)
	}
	if activeCards[0].ID != activeCard.ID || activeCards[0].Pronunciation != "/a/" {
		t.Fatalf("unexpected first active card: %#v", activeCards[0])
	}

	removedStatus := domain.CardStatusRemoved
	removedCards := mustListCardsForQueryTest(t, store, ctx, deck.ID, &removedStatus, "removed")
	if len(removedCards) != 1 || removedCards[0].ID != removedCard.ID {
		t.Fatalf("unexpected removed cards: %#v", removedCards)
	}
}

func TestDeckCardStats(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()
	deck := mustCreateDeckQueryTest(t, store, ctx, "Deck", "EN", "RU")

	dueCard := mustCreateCardQueryTest(t, store, ctx, deck.ID, "a", "a")
	postponedCard := mustCreateCardQueryTest(t, store, ctx, deck.ID, "b", "b")
	removedCard := mustCreateCardQueryTest(t, store, ctx, deck.ID, "c", "c")

	now := time.Now().UTC()
	mustUpdateCardScheduleQueryTest(t, store, ctx, dueCard.ID, now.Add(-time.Minute), 600, 2.3, 1, now)
	mustUpdateCardScheduleQueryTest(t, store, ctx, postponedCard.ID, now.Add(10*time.Minute), 600, 2.3, 1, now)
	mustSetCardStatusQueryTest(t, store, ctx, removedCard.ID, domain.CardStatusRemoved)

	stats, err := store.DeckCardStats(ctx, deck.ID, now)
	if err != nil {
		t.Fatalf("DeckCardStats: %v", err)
	}
	if stats.Active != 1 || stats.Postponed != 1 || stats.Total != 2 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

func mustCreateDeckQueryTest(t *testing.T, store *Store, ctx context.Context, name, languageFrom, languageTo string) domain.Deck {
	t.Helper()
	deck, err := store.CreateDeck(ctx, name, languageFrom, languageTo)
	if err != nil {
		t.Fatalf("CreateDeck: %v", err)
	}
	return deck
}

func mustCreateCardQueryTest(t *testing.T, store *Store, ctx context.Context, deckID int64, front, back string) domain.Card {
	t.Helper()
	c, err := store.CreateCard(ctx, CardCreateParams{DeckID: deckID, Front: front, Back: back})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	return c
}

func mustUpdateCardScheduleQueryTest(t *testing.T, store *Store, ctx context.Context, cardID int64, due time.Time, intervalSec int64, ease float64, reps int64, updated time.Time) {
	t.Helper()
	ok, err := store.UpdateCardSchedule(ctx, cardID, due, intervalSec, ease, reps, updated)
	if err != nil || !ok {
		t.Fatalf("UpdateCardSchedule: updated=%v err=%v", ok, err)
	}
}

func mustSetCardStatusQueryTest(t *testing.T, store *Store, ctx context.Context, cardID int64, status domain.CardStatus) {
	t.Helper()
	ok, err := store.SetCardStatus(ctx, cardID, status)
	if err != nil || !ok {
		t.Fatalf("SetCardStatus: updated=%v err=%v", ok, err)
	}
}

func TestNextCardForDeck_UsesDueDateOrder(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	deck, err := store.CreateDeck(ctx, "Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeck: %v", err)
	}

	first, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "first", Back: "one"})
	if err != nil {
		t.Fatalf("CreateCard first: %v", err)
	}
	second, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "second", Back: "two"})
	if err != nil {
		t.Fatalf("CreateCard second: %v", err)
	}

	if updated, err := store.UpdateCardSchedule(ctx, first.ID, now.Add(5*time.Minute), 300, 2.5, 0, now); err != nil || !updated {
		t.Fatalf("UpdateCardSchedule first: updated=%v err=%v", updated, err)
	}
	if updated, err := store.UpdateCardSchedule(ctx, second.ID, now.Add(-2*time.Minute), 300, 2.5, 0, now); err != nil || !updated {
		t.Fatalf("UpdateCardSchedule second: updated=%v err=%v", updated, err)
	}

	next, err := store.NextCardForDeck(ctx, deck.ID, now)
	if err != nil {
		t.Fatalf("NextCardForDeck: %v", err)
	}
	if next == nil || next.ID != second.ID {
		t.Fatalf("expected due card %d, got %#v", second.ID, next)
	}
}

func TestDeckOwnerIsolation(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	ownerOneDeck, err := store.CreateDeckForOwner(ctx, 101, "OwnerOne", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner owner1: %v", err)
	}
	if _, err := store.CreateDeckForOwner(ctx, 202, "OwnerTwo", "EN", "DE"); err != nil {
		t.Fatalf("CreateDeckForOwner owner2: %v", err)
	}

	ownerOneDecks, err := store.ListDecksForOwner(ctx, 101)
	if err != nil {
		t.Fatalf("ListDecksForOwner owner1: %v", err)
	}
	if len(ownerOneDecks) != 1 || ownerOneDecks[0].ID != ownerOneDeck.ID {
		t.Fatalf("unexpected owner1 decks: %#v", ownerOneDecks)
	}

	ownerTwoDecks, err := store.ListDecksForOwner(ctx, 202)
	if err != nil {
		t.Fatalf("ListDecksForOwner owner2: %v", err)
	}
	if len(ownerTwoDecks) != 1 {
		t.Fatalf("expected one owner2 deck, got %#v", ownerTwoDecks)
	}

	exists, err := store.DeckExistsForOwner(ctx, ownerOneDeck.ID, 202)
	if err != nil {
		t.Fatalf("DeckExistsForOwner mismatch owner: %v", err)
	}
	if exists {
		t.Fatal("did not expect owner2 to see owner1 deck")
	}
}

func TestListDecksAll(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	d0 := mustCreateDeckQueryTest(t, store, ctx, "CLI Deck", "EN", "RU")
	d1 := mustCreateDeckForOwnerQueryTest(t, store, ctx, 101, "Owner1", "EN", "DE")
	d2 := mustCreateDeckForOwnerQueryTest(t, store, ctx, 202, "Owner2", "DE", "RU")

	all, err := store.ListDecksAll(ctx)
	if err != nil {
		t.Fatalf("ListDecksAll: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 decks, got %d", len(all))
	}
	assertDeckRow(t, all[0], d0.ID, 0, "CLI Deck")
	if all[1].ID != d1.ID || all[1].TelegramUserID != 101 {
		t.Fatalf("unexpected second deck: %#v", all[1])
	}
	if all[2].ID != d2.ID || all[2].TelegramUserID != 202 {
		t.Fatalf("unexpected third deck: %#v", all[2])
	}
}

func assertDeckRow(t *testing.T, row domain.Deck, id int64, telegramUserID int64, name string) {
	t.Helper()
	if row.ID != id || row.TelegramUserID != telegramUserID || row.Name != name {
		t.Fatalf("unexpected deck: %#v", row)
	}
}

func TestGetDeckByID(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	d1, err := store.CreateDeckForOwner(ctx, 101, "Deck1", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}

	got, err := store.GetDeckByID(ctx, d1.ID)
	if err != nil {
		t.Fatalf("GetDeckByID: %v", err)
	}
	if got == nil || got.ID != d1.ID || got.TelegramUserID != 101 || got.Name != "Deck1" {
		t.Fatalf("unexpected deck: %#v", got)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("deck read from store must have non-zero CreatedAt and UpdatedAt; got CreatedAt=%v UpdatedAt=%v", got.CreatedAt, got.UpdatedAt)
	}

	missing, err := store.GetDeckByID(ctx, d1.ID+9999)
	if err != nil {
		t.Fatalf("GetDeckByID missing: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing deck, got %#v", missing)
	}
}

func TestCardFrontExistsInDeckForOwner(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	deck, err := store.CreateDeckForOwner(ctx, 101, "OwnerOne", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner owner1: %v", err)
	}
	if _, err := store.CreateCard(ctx, CardCreateParams{
		DeckID: deck.ID, Front: "Banished", Back: "изгнанный", Pronunciation: "/banished/", Example: "",
	}); err != nil {
		t.Fatalf("CreateCard: %v", err)
	}

	exists, err := store.CardFrontExistsInDeckForOwner(ctx, deck.ID, 101, "  banished ")
	if err != nil {
		t.Fatalf("CardFrontExistsInDeckForOwner owner1: %v", err)
	}
	if !exists {
		t.Fatal("expected existing front for owner1")
	}

	exists, err = store.CardFrontExistsInDeckForOwner(ctx, deck.ID, 202, "banished")
	if err != nil {
		t.Fatalf("CardFrontExistsInDeckForOwner owner2: %v", err)
	}
	if exists {
		t.Fatal("did not expect owner2 to see owner1 card front")
	}
}

func TestActiveDeckForUser(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	d1 := mustCreateDeckForOwnerQueryTest(t, store, ctx, 101, "Deck One", "EN", "RU")
	d2 := mustCreateDeckForOwnerQueryTest(t, store, ctx, 101, "Deck Two", "EN", "DE")

	mustSetActiveDeckQueryTest(t, store, ctx, 101, d1.ID)
	assertActiveDeckIs(t, store, ctx, 101, d1.ID)

	mustSetActiveDeckQueryTest(t, store, ctx, 101, d2.ID)
	assertActiveDeckIs(t, store, ctx, 101, d2.ID)

	if err := store.ClearActiveDeckForUser(ctx, 101); err != nil {
		t.Fatalf("ClearActiveDeckForUser: %v", err)
	}
	assertActiveDeckNil(t, store, ctx, 101)
}

func mustSetActiveDeckQueryTest(t *testing.T, store *Store, ctx context.Context, userID, deckID int64) {
	t.Helper()
	if err := store.SetActiveDeckForUser(ctx, userID, deckID); err != nil {
		t.Fatalf("SetActiveDeckForUser: %v", err)
	}
}

func assertActiveDeckIs(t *testing.T, store *Store, ctx context.Context, userID, deckID int64) {
	t.Helper()
	got, err := store.GetActiveDeckForUser(ctx, userID)
	if err != nil {
		t.Fatalf("GetActiveDeckForUser: %v", err)
	}
	if got == nil || got.ID != deckID {
		t.Fatalf("expected active deck %d, got %#v", deckID, got)
	}
}

func assertActiveDeckNil(t *testing.T, store *Store, ctx context.Context, userID int64) {
	t.Helper()
	got, err := store.GetActiveDeckForUser(ctx, userID)
	if err != nil {
		t.Fatalf("GetActiveDeckForUser: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil active deck, got %#v", got)
	}
}

func TestFindDeckByNameForOwner(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	mustCreateDeckForOwnerQueryTest(t, store, ctx, 101, "English Basics", "EN", "RU")
	mustCreateDeckForOwnerQueryTest(t, store, ctx, 101, "English Advanced", "EN", "DE")
	mustCreateDeckForOwnerQueryTest(t, store, ctx, 101, "Basic Portuguese", "PT", "RU")
	mustCreateDeckForOwnerQueryTest(t, store, ctx, 202, "English Basics", "EN", "RU")

	exact, err := store.FindDeckByExactNameForOwner(ctx, 101, " english basics ")
	if err != nil {
		t.Fatalf("FindDeckByExactNameForOwner: %v", err)
	}
	if exact == nil || exact.Name != "English Basics" || exact.TelegramUserID != 101 {
		t.Fatalf("unexpected exact deck: %#v", exact)
	}

	missing, err := store.FindDeckByExactNameForOwner(ctx, 101, "nonexistent")
	if err != nil {
		t.Fatalf("FindDeckByExactNameForOwner missing: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil exact deck, got %#v", missing)
	}

	assertDeckCandidatesForOwner(t, store, ctx, 101, "english", 10, 2)
}

func assertDeckCandidatesForOwner(t *testing.T, store *Store, ctx context.Context, ownerID int64, query string, limit, wantLen int) {
	t.Helper()
	candidates, err := store.FindDeckCandidatesForOwner(ctx, ownerID, query, limit)
	if err != nil {
		t.Fatalf("FindDeckCandidatesForOwner: %v", err)
	}
	if len(candidates) != wantLen {
		t.Fatalf("expected %d candidates, got %#v", wantLen, candidates)
	}
	for _, d := range candidates {
		if d.TelegramUserID != ownerID {
			t.Fatalf("expected owner %d candidate, got %#v", ownerID, d)
		}
	}
}

func TestBackfillEntriesPreferLatestFromLegacyCards(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "legacy-backfill.db")
	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()

	mustExecQueryTest(t, store, ctx, `
		CREATE TABLE decks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			telegram_user_id INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL,
			language_from TEXT NOT NULL,
			language_to TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE cards (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			deck_id INTEGER NOT NULL,
			front TEXT NOT NULL,
			back TEXT NOT NULL,
			pronunciation TEXT NOT NULL DEFAULT '',
			example TEXT NOT NULL DEFAULT '',
			conjugation TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			next_due_at DATETIME NULL,
			interval_sec INTEGER NOT NULL DEFAULT 0,
			ease REAL NOT NULL DEFAULT 2.5,
			lapses INTEGER NOT NULL DEFAULT 0,
			last_reviewed_at DATETIME NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`)

	deck, err := store.CreateDeckForOwner(ctx, 101, "Legacy", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}
	mustInsertLegacyCard(t, store, ctx, deck.ID, "banished", "old-back", "/b/", "old ex", "")
	mustInsertLegacyCard(t, store, ctx, deck.ID, "banished", "new-back", "/b2/", "new ex", "form")

	if err := store.InitSchema(ctx); err != nil {
		t.Fatalf("InitSchema legacy backfill: %v", err)
	}

	entriesCount := mustCountInt64QueryTest(t, store, ctx, `SELECT COUNT(1) FROM entries WHERE language_from='EN' AND language_to='RU' AND front_norm='banished'`, "count entries")
	if entriesCount != 1 {
		t.Fatalf("expected exactly one entry, got %d", entriesCount)
	}

	back, pron, ex, conj := mustLoadEntryFieldsByNorm(t, store, ctx, "EN", "RU", "banished")
	if back != "new-back" || pron != "/b2/" || ex != "new ex" || conj != "form" {
		t.Fatalf("expected prefer-latest entry values, got back=%q pron=%q ex=%q conj=%q", back, pron, ex, conj)
	}

	missing := mustCountInt64QueryTest(t, store, ctx, `SELECT COUNT(1) FROM cards WHERE entry_id IS NULL`, "count missing entry_id")
	if missing != 0 {
		t.Fatalf("expected 0 cards with NULL entry_id, got %d", missing)
	}
}

func mustCreateCardForQueryTest(t *testing.T, store *Store, ctx context.Context, deckID int64, front, back, pronunciation, example, conjugation string) domain.Card {
	t.Helper()
	card, err := store.CreateCard(ctx, CardCreateParams{
		DeckID:        deckID,
		Front:         front,
		Back:          back,
		Pronunciation: pronunciation,
		Example:       example,
		Conjugation:   conjugation,
	})
	if err != nil {
		t.Fatalf("CreateCard %q: %v", front, err)
	}
	return card
}

func mustUpdateCardScheduleForQueryTest(t *testing.T, store *Store, ctx context.Context, cardID int64, due time.Time, interval int64, ease float64, lapses int64, label string) {
	t.Helper()
	updated, err := store.UpdateCardSchedule(ctx, cardID, due, interval, ease, lapses, time.Now().UTC())
	if err != nil || !updated {
		t.Fatalf("UpdateCardSchedule %s: updated=%v err=%v", label, updated, err)
	}
}

func mustSetCardStatusForQueryTest(t *testing.T, store *Store, ctx context.Context, cardID int64, status domain.CardStatus) {
	t.Helper()
	updated, err := store.SetCardStatus(ctx, cardID, status)
	if err != nil || !updated {
		t.Fatalf("SetCardStatus: updated=%v err=%v", updated, err)
	}
}

func mustListCardsForQueryTest(t *testing.T, store *Store, ctx context.Context, deckID int64, status *domain.CardStatus, label string) []domain.Card {
	t.Helper()
	cards, err := store.ListCards(ctx, deckID, status)
	if err != nil {
		t.Fatalf("ListCards %s: %v", label, err)
	}
	return cards
}

func assertCardCountForDeck(t *testing.T, store *Store, ctx context.Context, deckID int64, status *domain.CardStatus, want int, label string) {
	t.Helper()
	cards := mustListCardsForQueryTest(t, store, ctx, deckID, status, label)
	if len(cards) != want {
		t.Fatalf("expected %d cards for %s, got %d", want, label, len(cards))
	}
}

func mustExecQueryTest(t *testing.T, store *Store, ctx context.Context, statement string, args ...any) {
	t.Helper()
	if _, err := store.DB().ExecContext(ctx, statement, args...); err != nil {
		t.Fatalf("exec statement: %v", err)
	}
}

func mustInsertLegacyCard(t *testing.T, store *Store, ctx context.Context, deckID int64, front, back, pronunciation, example, conjugation string) {
	t.Helper()
	mustExecQueryTest(
		t,
		store,
		ctx,
		`INSERT INTO cards (deck_id, front, back, pronunciation, example, conjugation, status) VALUES (?, ?, ?, ?, ?, ?, 'active')`,
		deckID,
		front,
		back,
		pronunciation,
		example,
		conjugation,
	)
}

func mustCountInt64QueryTest(t *testing.T, store *Store, ctx context.Context, query, label string, args ...any) int64 {
	t.Helper()
	var value int64
	if err := store.DB().QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	return value
}

func mustLoadEntryFieldsByNorm(t *testing.T, store *Store, ctx context.Context, from, to, norm string) (string, string, string, string) {
	t.Helper()
	var back, pronunciation, example, conjugation string
	if err := store.DB().QueryRowContext(
		ctx,
		`SELECT back, pronunciation, example, conjugation
		 FROM entries
		 WHERE language_from = ? AND language_to = ? AND front_norm = ?`,
		from,
		to,
		norm,
	).Scan(&back, &pronunciation, &example, &conjugation); err != nil {
		t.Fatalf("select entry values: %v", err)
	}
	return back, pronunciation, example, conjugation
}

func TestCreateCard_UsesSharedEntryAcrossUsers(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	d1, err := store.CreateDeckForOwner(ctx, 101, "User1", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner d1: %v", err)
	}
	d2, err := store.CreateDeckForOwner(ctx, 202, "User2", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner d2: %v", err)
	}

	c1, err := store.CreateCard(ctx, CardCreateParams{
		DeckID:        d1.ID,
		Front:         "banished",
		Back:          "old-back",
		Pronunciation: "/b/",
		Example:       "old ex",
		Conjugation:   "",
	})
	if err != nil {
		t.Fatalf("CreateCard c1: %v", err)
	}
	c2, err := store.CreateCard(ctx, CardCreateParams{
		DeckID:        d2.ID,
		Front:         "banished",
		Back:          "new-back",
		Pronunciation: "/b2/",
		Example:       "new ex",
		Conjugation:   "forms",
	})
	if err != nil {
		t.Fatalf("CreateCard c2: %v", err)
	}

	if c1.EntryID == 0 || c2.EntryID == 0 {
		t.Fatalf("expected non-zero entry ids, got c1=%d c2=%d", c1.EntryID, c2.EntryID)
	}
	if c1.EntryID != c2.EntryID {
		t.Fatalf("expected shared entry id, got c1=%d c2=%d", c1.EntryID, c2.EntryID)
	}

	refetched, err := store.GetCardByID(ctx, c1.ID)
	if err != nil {
		t.Fatalf("GetCardByID c1: %v", err)
	}
	if refetched == nil {
		t.Fatal("expected refetched card")
	}
	// Shared live dictionary: latest upserted entry text is visible to both users.
	if refetched.Back != "new-back" || refetched.Example != "new ex" {
		t.Fatalf("expected shared live entry values, got back=%q example=%q", refetched.Back, refetched.Example)
	}
}

func TestRegenerateRemovedCardForOwner_ReactivatesAndUpdatesEntry(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	deck := mustCreateDeckForOwnerQueryTest(t, store, ctx, 101, "Owner Deck", "EN", "RU")
	card := mustCreateCardForQueryTest(t, store, ctx, deck.ID, "banished", "old-back", "/old/", "old ex", "")
	if updated, err := store.SetCardStatus(ctx, card.ID, domain.CardStatusRemoved); err != nil || !updated {
		t.Fatalf("SetCardStatus removed: updated=%v err=%v", updated, err)
	}

	regenerated, err := store.RegenerateRemovedCardForOwner(
		ctx,
		deck.ID,
		101,
		"BANISHED",
		"new-back",
		"/new/",
		"new example",
		"forms",
		now,
	)
	if err != nil {
		t.Fatalf("RegenerateRemovedCardForOwner: %v", err)
	}
	if !regenerated {
		t.Fatal("expected removed card to be regenerated")
	}

	got := mustGetCardByIDForOwnerQueryTest(t, store, ctx, card.ID, 101)
	assertRegeneratedCardFields(t, got, now)
}

func TestRegenerateRemovedCardForOwner_ActiveDuplicateReturnsFalse(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	deck, err := store.CreateDeckForOwner(ctx, 101, "Owner Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}
	removed, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "banished", Back: "old"})
	if err != nil {
		t.Fatalf("CreateCard removed: %v", err)
	}
	if updated, err := store.SetCardStatus(ctx, removed.ID, domain.CardStatusRemoved); err != nil || !updated {
		t.Fatalf("SetCardStatus removed: updated=%v err=%v", updated, err)
	}
	if _, err := store.CreateCard(ctx, CardCreateParams{DeckID: deck.ID, Front: "banished", Back: "active"}); err != nil {
		t.Fatalf("CreateCard active duplicate: %v", err)
	}

	regenerated, err := store.RegenerateRemovedCardForOwner(
		ctx,
		deck.ID,
		101,
		"banished",
		"new-back",
		"/new/",
		"new example",
		"",
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf("RegenerateRemovedCardForOwner: %v", err)
	}
	if regenerated {
		t.Fatal("expected regenerate=false when active duplicate exists")
	}

	removedAfter, err := store.GetCardByIDForOwner(ctx, removed.ID, 101)
	if err != nil {
		t.Fatalf("GetCardByIDForOwner removed: %v", err)
	}
	if removedAfter == nil || removedAfter.Status != domain.CardStatusRemoved {
		t.Fatalf("expected removed card unchanged, got %#v", removedAfter)
	}
}

func TestRegenerateRemovedCardForOwner_InvalidFront(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	ctx := context.Background()

	deck, err := store.CreateDeckForOwner(ctx, 101, "Owner Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}
	regenerated, err := store.RegenerateRemovedCardForOwner(ctx, deck.ID, 101, "   ", "b", "", "", "", time.Now().UTC())
	if err == nil {
		t.Fatal("expected validation error for empty front")
	}
	if regenerated {
		t.Fatal("expected regenerate=false for invalid input")
	}
}

func mustCreateDeckForOwnerQueryTest(
	t *testing.T,
	store *Store,
	ctx context.Context,
	telegramUserID int64,
	name string,
	languageFrom string,
	languageTo string,
) domain.Deck {
	t.Helper()
	deck, err := store.CreateDeckForOwner(ctx, telegramUserID, name, languageFrom, languageTo)
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}
	return deck
}

func mustGetCardByIDForOwnerQueryTest(t *testing.T, store *Store, ctx context.Context, cardID int64, telegramUserID int64) *domain.Card {
	t.Helper()
	card, err := store.GetCardByIDForOwner(ctx, cardID, telegramUserID)
	if err != nil {
		t.Fatalf("GetCardByIDForOwner: %v", err)
	}
	return card
}

func assertRegeneratedCardFields(t *testing.T, got *domain.Card, now time.Time) {
	t.Helper()
	if got == nil {
		t.Fatal("expected card to exist after regeneration")
	}
	if got.Status != domain.CardStatusActive {
		t.Fatalf("expected active status, got %q", got.Status)
	}
	if got.Back != "new-back" || got.Pronunciation != "/new/" || got.Example != "new example" || got.Conjugation != "forms" {
		t.Fatalf("expected updated generated fields, got %#v", got)
	}
	if got.NextDueAt.Before(now.Add(-1*time.Second)) || got.NextDueAt.After(now.Add(1*time.Second)) {
		t.Fatalf("expected due time around %s, got %s", now.Format(time.RFC3339), got.NextDueAt.Format(time.RFC3339))
	}
}
