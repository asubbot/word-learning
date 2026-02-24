package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"word-learning-cli/internal/ai"
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
			return ai.GeneratedCard{Back: "translated-" + req.Front}, nil
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
			return ai.GeneratedCard{Back: "   "}, nil
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
			return ai.GeneratedCard{Back: "translated-" + req.Front}, nil
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
