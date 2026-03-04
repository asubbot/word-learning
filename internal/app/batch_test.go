package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"word-learning/internal/ai"
	"word-learning/internal/domain"
)

type fakeGenerator struct {
	generate func(req ai.GenerateCardRequest) (ai.GeneratedCard, error)
}

func (f fakeGenerator) GenerateCard(ctx context.Context, req ai.GenerateCardRequest) (ai.GeneratedCard, error) {
	_ = ctx
	return f.generate(req)
}

func TestBatchAddReportCountersDeterministic(t *testing.T) {
	t.Parallel()

	items := []BatchAddItemResult{
		{Status: BatchAddStatusCreated},
		{Status: BatchAddStatusDuplicate},
		{Status: BatchAddStatusFailedGeneration},
		{Status: BatchAddStatusFailedValidation},
		{Status: BatchAddStatusCreated},
	}

	var report BatchAddReport
	for _, item := range items {
		report.AddItem(item)
	}

	if report.Summary.Total != 5 {
		t.Fatalf("expected total=5, got %d", report.Summary.Total)
	}
	if report.Summary.Created != 2 {
		t.Fatalf("expected created=2, got %d", report.Summary.Created)
	}
	if report.Summary.SkippedDuplicates != 1 {
		t.Fatalf("expected skipped_duplicates=1, got %d", report.Summary.SkippedDuplicates)
	}
	if report.Summary.Failed != 2 {
		t.Fatalf("expected failed=2, got %d", report.Summary.Failed)
	}
}

func TestNormalizeBatchFrontsCLI(t *testing.T) {
	t.Parallel()

	input := []string{
		"  banished  ",
		"",
		"   ",
		"# comment",
		"  #comment-with-space",
		"come up with",
	}
	got := NormalizeBatchFronts(input, BatchModeCLI)
	want := []string{"banished", "come up with"}
	if len(got) != len(want) {
		t.Fatalf("expected %d normalized lines, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d mismatch: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestNormalizeBatchFrontsBotKeepsHashLines(t *testing.T) {
	t.Parallel()

	input := []string{
		"  banished ",
		"# hash-is-data-for-bot",
		"  ",
		" crack down on sth ",
	}
	got := NormalizeBatchFronts(input, BatchModeBot)
	want := []string{"banished", "# hash-is-data-for-bot", "crack down on sth"}
	if len(got) != len(want) {
		t.Fatalf("expected %d normalized lines, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d mismatch: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestAddCardsBatchAIForUser_AllSuccess(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()
	deckID := mustCreateDeck(t, svc)
	generator := fakeGenerator{
		generate: func(req ai.GenerateCardRequest) (ai.GeneratedCard, error) {
			return ai.GeneratedCard{
				Front:         req.Front,
				Back:          "translated-" + req.Front,
				Pronunciation: "/p/",
				Example:       "d",
				Conjugation:   "",
			}, nil
		},
	}

	report, err := svc.AddCardsBatchAIForUser(ctx, 0, generator, BatchAddAIParams{
		DeckID: deckID,
		Lines:  []string{"banished", "come up with"},
		Mode:   BatchModeCLI,
		DryRun: false,
	})
	if err != nil {
		t.Fatalf("AddCardsBatchAIForUser: %v", err)
	}
	if report.Summary.Total != 2 || report.Summary.Created != 2 || report.Summary.SkippedDuplicates != 0 || report.Summary.Failed != 0 {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}
	cards, err := svc.ListCards(ctx, deckID, "active")
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 created cards, got %d", len(cards))
	}
}

func TestAddCardsBatchAI(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()
	deckID := mustCreateDeck(t, svc)
	const wantFront, wantBack = "word", "translation"

	generator := fakeGenerator{
		generate: func(req ai.GenerateCardRequest) (ai.GeneratedCard, error) {
			return ai.GeneratedCard{
				Front: req.Front,
				Back:  wantBack,
			}, nil
		},
	}

	report, err := svc.AddCardsBatchAI(ctx, generator, BatchAddAIParams{
		DeckID: deckID,
		Lines:  []string{"word"},
		Mode:   BatchModeCLI,
		DryRun: false,
	})
	if err != nil {
		t.Fatalf("AddCardsBatchAI: %v", err)
	}
	if report.Summary.Created < 1 {
		t.Fatalf("expected Summary.Created >= 1, got %d", report.Summary.Created)
	}
	cards, err := svc.ListCards(ctx, deckID, "active")
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card in store, got %d", len(cards))
	}
	if cards[0].Front != wantFront || cards[0].Back != wantBack {
		t.Fatalf("unexpected card: front=%q back=%q", cards[0].Front, cards[0].Back)
	}
}

func TestAddCardsBatchAIForUser_DuplicateSkip(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()
	deckID := mustCreateDeck(t, svc)
	if _, err := svc.AddCard(ctx, deckID, "duplicate", "one", "", "", ""); err != nil {
		t.Fatalf("seed duplicate: %v", err)
	}
	generator := fakeGenerator{
		generate: func(req ai.GenerateCardRequest) (ai.GeneratedCard, error) {
			return ai.GeneratedCard{Front: req.Front, Back: "translated-" + req.Front}, nil
		},
	}

	report, err := svc.AddCardsBatchAIForUser(ctx, 0, generator, BatchAddAIParams{
		DeckID: deckID,
		Lines:  []string{"  DuPlicate  "},
		Mode:   BatchModeCLI,
	})
	if err != nil {
		t.Fatalf("AddCardsBatchAIForUser: %v", err)
	}
	if report.Summary.SkippedDuplicates != 1 || report.Summary.Created != 0 || report.Summary.Failed != 0 {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}
}

func TestAddCardsBatchAIForUser_RemovedDuplicateRegeneratedAndReactivated(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()
	deckID := mustCreateDeck(t, svc)
	seed, err := svc.AddCard(ctx, deckID, "duplicate", "old-back", "/old/", "old example", "")
	if err != nil {
		t.Fatalf("seed card: %v", err)
	}
	if err := svc.RemoveCard(ctx, seed.ID); err != nil {
		t.Fatalf("remove seed card: %v", err)
	}

	generator := fakeGenerator{
		generate: func(req ai.GenerateCardRequest) (ai.GeneratedCard, error) {
			return ai.GeneratedCard{
				Front:         req.Front,
				Back:          "new-back",
				Pronunciation: "/new/",
				Example:       "new example",
				Conjugation:   "",
			}, nil
		},
	}

	report, err := svc.AddCardsBatchAIForUser(ctx, 0, generator, BatchAddAIParams{
		DeckID: deckID,
		Lines:  []string{"duplicate"},
		Mode:   BatchModeCLI,
	})
	if err != nil {
		t.Fatalf("AddCardsBatchAIForUser: %v", err)
	}
	assertBatchSummary(t, report.Summary, 1, 0, 0)

	allCards, err := svc.ListCards(ctx, deckID, "")
	if err != nil {
		t.Fatalf("ListCards all: %v", err)
	}
	if len(allCards) != 1 {
		t.Fatalf("expected exactly one card after regeneration, got %d", len(allCards))
	}
	assertRegeneratedCardFields(t, allCards[0], "new-back", "/new/", "new example")
}

func assertBatchSummary(t *testing.T, s BatchAddSummary, created, skipped, failed int) {
	t.Helper()
	if s.Created != created || s.SkippedDuplicates != skipped || s.Failed != failed {
		t.Fatalf("unexpected summary: created=%d skipped=%d failed=%d (want %d %d %d)", s.Created, s.SkippedDuplicates, s.Failed, created, skipped, failed)
	}
}

func assertRegeneratedCardFields(t *testing.T, c domain.Card, back, pronunciation, example string) {
	t.Helper()
	if c.Status != "active" {
		t.Fatalf("expected card status active, got %q", c.Status)
	}
	if c.Back != back || c.Pronunciation != pronunciation || c.Example != example {
		t.Fatalf("expected regenerated fields back=%q pron=%q ex=%q, got %#v", back, pronunciation, example, c)
	}
}

func TestAddCardsBatchAIForUser_GenerationFailure(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()
	deckID := mustCreateDeck(t, svc)
	generator := fakeGenerator{
		generate: func(req ai.GenerateCardRequest) (ai.GeneratedCard, error) {
			return ai.GeneratedCard{}, fmt.Errorf("provider timeout")
		},
	}

	report, err := svc.AddCardsBatchAIForUser(ctx, 0, generator, BatchAddAIParams{
		DeckID: deckID,
		Lines:  []string{"banished"},
		Mode:   BatchModeCLI,
	})
	if err != nil {
		t.Fatalf("AddCardsBatchAIForUser: %v", err)
	}
	if report.Summary.Failed != 1 || report.Items[0].Status != BatchAddStatusFailedGeneration {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestAddCardsBatchAIForUser_ValidationFailure(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()
	deckID := mustCreateDeck(t, svc)
	generator := fakeGenerator{
		generate: func(req ai.GenerateCardRequest) (ai.GeneratedCard, error) {
			return ai.GeneratedCard{Front: req.Front, Back: "   "}, nil
		},
	}

	report, err := svc.AddCardsBatchAIForUser(ctx, 0, generator, BatchAddAIParams{
		DeckID: deckID,
		Lines:  []string{"banished"},
		Mode:   BatchModeCLI,
	})
	if err != nil {
		t.Fatalf("AddCardsBatchAIForUser: %v", err)
	}
	if report.Summary.Failed != 1 || report.Items[0].Status != BatchAddStatusFailedValidation {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestAddCardsBatchAIForUser_DryRunNoPersistence(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()
	deckID := mustCreateDeck(t, svc)
	generator := fakeGenerator{
		generate: func(req ai.GenerateCardRequest) (ai.GeneratedCard, error) {
			return ai.GeneratedCard{Front: req.Front, Back: "translated-" + req.Front}, nil
		},
	}

	report, err := svc.AddCardsBatchAIForUser(ctx, 0, generator, BatchAddAIParams{
		DeckID: deckID,
		Lines:  []string{"banished", "come up with"},
		Mode:   BatchModeCLI,
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("AddCardsBatchAIForUser: %v", err)
	}
	if report.Summary.Created != 2 || report.Summary.Failed != 0 || report.Summary.SkippedDuplicates != 0 {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}

	cards, err := svc.ListCards(ctx, deckID, "active")
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("expected no persisted cards in dry-run, got %d", len(cards))
	}
}

func TestAddCardsBatchAIToDeck_BotDeck(t *testing.T) {
	t.Parallel()

	svc, store := newTestService(t)
	ctx := context.Background()
	botDeck, err := store.CreateDeckForOwner(ctx, 101, "Bot Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForOwner: %v", err)
	}
	generator := fakeGenerator{
		generate: func(req ai.GenerateCardRequest) (ai.GeneratedCard, error) {
			return ai.GeneratedCard{
				Front:         req.Front,
				Back:          "translated-" + req.Front,
				Pronunciation: "/p/",
				Example:       "d",
				Conjugation:   "",
			}, nil
		},
	}

	report, err := svc.AddCardsBatchAIToDeck(ctx, generator, BatchAddAIParams{
		DeckID: botDeck.ID,
		Lines:  []string{"word1", "word2"},
		Mode:   BatchModeCLI,
		DryRun: false,
	})
	if err != nil {
		t.Fatalf("AddCardsBatchAIToDeck: %v", err)
	}
	if report.Summary.Total != 2 || report.Summary.Created != 2 || report.Summary.Failed != 0 {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}

	cards, err := svc.ListCardsInDeck(ctx, botDeck.ID, "active")
	if err != nil {
		t.Fatalf("ListCardsInDeck: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards in bot deck, got %d", len(cards))
	}
}

func TestAddCardsBatchAIToDeck_DeckNotFound(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()
	generator := fakeGenerator{
		generate: func(req ai.GenerateCardRequest) (ai.GeneratedCard, error) {
			return ai.GeneratedCard{Back: "x"}, nil
		},
	}

	_, err := svc.AddCardsBatchAIToDeck(ctx, generator, BatchAddAIParams{
		DeckID: 99999,
		Lines:  []string{"word"},
		Mode:   BatchModeCLI,
	})
	if err == nil {
		t.Fatal("expected error for non-existent deck")
	}
	if !strings.Contains(err.Error(), "99999") || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAddCardsBatchAIForActiveDeckForUser(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()

	generator := fakeGenerator{
		generate: func(req ai.GenerateCardRequest) (ai.GeneratedCard, error) {
			return ai.GeneratedCard{
				Front:         req.Front,
				Back:          "translated-" + req.Front,
				Pronunciation: "/p/",
				Example:       "ex",
				Conjugation:   "",
			}, nil
		},
	}

	if _, err := svc.AddCardsBatchAIForActiveDeckForUser(ctx, 101, generator, []string{"a"}, BatchModeCLI, false); !errors.Is(err, ErrActiveDeckNotSet) {
		t.Fatalf("expected ErrActiveDeckNotSet, got %v", err)
	}

	deck, err := svc.CreateDeckForUser(ctx, 101, "Active Batch", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForUser: %v", err)
	}
	if _, err := svc.DeckUseForUser(ctx, 101, deck.Name); err != nil {
		t.Fatalf("DeckUseForUser: %v", err)
	}

	report, err := svc.AddCardsBatchAIForActiveDeckForUser(ctx, 101, generator, []string{"banished", "come up with"}, BatchModeCLI, false)
	if err != nil {
		t.Fatalf("AddCardsBatchAIForActiveDeckForUser: %v", err)
	}
	if report.Summary.Created != 2 || report.Summary.Failed != 0 {
		t.Fatalf("unexpected summary: %#v", report.Summary)
	}
}

func TestAddCardsBatchAIForUser_UsesGeneratedFrontForPersistence(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService(t)
	ctx := context.Background()
	deckID := mustCreateDeck(t, svc)
	input := "The president swore to CRACK DOWN ON corruption."
	generator := fakeGenerator{
		generate: func(req ai.GenerateCardRequest) (ai.GeneratedCard, error) {
			return ai.GeneratedCard{
				Front:         "crack down on",
				Back:          "жестко пресекать",
				Pronunciation: "/kræk daʊn ɒn/",
				Example:       req.Front,
				Conjugation:   "",
			}, nil
		},
	}

	report, err := svc.AddCardsBatchAIForUser(ctx, 0, generator, BatchAddAIParams{
		DeckID: deckID,
		Lines:  []string{input},
		Mode:   BatchModeCLI,
	})
	if err != nil {
		t.Fatalf("AddCardsBatchAIForUser: %v", err)
	}
	if report.Summary.Created != 1 || report.Items[0].FrontNormalized != "crack down on" {
		t.Fatalf("unexpected report: %#v", report)
	}

	cards, err := svc.ListCards(ctx, deckID, "active")
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(cards))
	}
	if cards[0].Front != "crack down on" {
		t.Fatalf("expected generated front to be persisted, got %q", cards[0].Front)
	}
	if cards[0].Example != input {
		t.Fatalf("expected input context in example, got %q", cards[0].Example)
	}
}
