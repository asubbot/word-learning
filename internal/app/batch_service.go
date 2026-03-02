package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"word-learning-cli/internal/ai"
)

type BatchAddAIParams struct {
	DeckID int64
	Lines  []string
	Mode   BatchMode
	DryRun bool
}

func (s *Service) AddCardsBatchAI(ctx context.Context, generator ai.Generator, params BatchAddAIParams) (BatchAddReport, error) {
	return s.AddCardsBatchAIForUser(ctx, 0, generator, params)
}

func (s *Service) AddCardsBatchAIForActiveDeckForUser(ctx context.Context, telegramUserID int64, generator ai.Generator, lines []string, mode BatchMode, dryRun bool) (BatchAddReport, error) {
	deck, err := s.ResolveActiveDeckForUser(ctx, telegramUserID)
	if err != nil {
		return BatchAddReport{}, err
	}
	return s.AddCardsBatchAIForUser(ctx, telegramUserID, generator, BatchAddAIParams{
		DeckID: deck.ID,
		Lines:  lines,
		Mode:   mode,
		DryRun: dryRun,
	})
}

func (s *Service) AddCardsBatchAIToDeck(ctx context.Context, generator ai.Generator, params BatchAddAIParams) (BatchAddReport, error) {
	deck, err := s.store.GetDeckByID(ctx, params.DeckID)
	if err != nil {
		return BatchAddReport{}, err
	}
	if deck == nil {
		return BatchAddReport{}, fmt.Errorf("deck %d does not exist", params.DeckID)
	}
	return s.AddCardsBatchAIForUser(ctx, deck.TelegramUserID, generator, params)
}

func (s *Service) AddCardsBatchAIForUser(ctx context.Context, telegramUserID int64, generator ai.Generator, params BatchAddAIParams) (BatchAddReport, error) {
	if params.DeckID <= 0 {
		return BatchAddReport{}, fmt.Errorf("--deck must be a positive integer")
	}
	if generator == nil {
		return BatchAddReport{}, fmt.Errorf("ai generator is required")
	}

	deck, err := s.store.GetDeckForOwner(ctx, params.DeckID, telegramUserID)
	if err != nil {
		return BatchAddReport{}, err
	}
	if deck == nil {
		return BatchAddReport{}, fmt.Errorf("deck %d does not exist", params.DeckID)
	}

	fronts := NormalizeBatchFronts(params.Lines, params.Mode)
	report := BatchAddReport{Items: make([]BatchAddItemResult, 0, len(fronts))}
	for _, front := range fronts {
		item := BatchAddItemResult{
			FrontRaw:        front,
			FrontNormalized: front,
		}
		generated, genErr := generator.GenerateCard(ctx, ai.GenerateCardRequest{
			LanguageFrom: deck.LanguageFrom,
			LanguageTo:   deck.LanguageTo,
			Front:        front,
		})
		if genErr != nil {
			item.Status = BatchAddStatusFailedGeneration
			item.Reason = genErr.Error()
			report.AddItem(item)
			continue
		}

		generatedFront := strings.TrimSpace(generated.Front)
		back := strings.TrimSpace(generated.Back)
		pronunciation := strings.TrimSpace(generated.Pronunciation)
		example := strings.TrimSpace(generated.Example)
		conjugation := strings.TrimSpace(generated.Conjugation)
		if generatedFront == "" {
			item.Status = BatchAddStatusFailedValidation
			item.Reason = "generated front is empty"
			report.AddItem(item)
			continue
		}
		if back == "" {
			item.Status = BatchAddStatusFailedValidation
			item.Reason = "generated back is empty"
			report.AddItem(item)
			continue
		}
		item.FrontNormalized = generatedFront

		if params.DryRun {
			item.Status = BatchAddStatusCreated
			report.AddItem(item)
			continue
		}

		_, addErr := s.AddCardForUser(ctx, telegramUserID, params.DeckID, generatedFront, back, pronunciation, example, conjugation)
		if addErr == nil {
			item.Status = BatchAddStatusCreated
			report.AddItem(item)
			continue
		}
		if errors.Is(addErr, ErrCardAlreadyExists) {
			regenerated, regenErr := s.store.RegenerateRemovedCardForOwner(
				ctx,
				params.DeckID,
				telegramUserID,
				generatedFront,
				back,
				pronunciation,
				example,
				conjugation,
				time.Now().UTC(),
			)
			if regenErr != nil {
				item.Status = BatchAddStatusFailedValidation
				item.Reason = regenErr.Error()
				report.AddItem(item)
				continue
			}
			if regenerated {
				item.Status = BatchAddStatusCreated
				report.AddItem(item)
				continue
			}
			item.Status = BatchAddStatusDuplicate
			item.Reason = addErr.Error()
			report.AddItem(item)
			continue
		}
		item.Status = BatchAddStatusFailedValidation
		item.Reason = addErr.Error()
		report.AddItem(item)
	}

	return report, nil
}
