package bot

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"word-learning-cli/internal/ai"
	"word-learning-cli/internal/app"
	"word-learning-cli/internal/storage/sqlite"
)

type fakeAPI struct {
	sentTexts     []string
	sentConfigs   []tgbotapi.MessageConfig
	callbackTexts []string
}

func (f *fakeAPI) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	if msg, ok := c.(tgbotapi.MessageConfig); ok {
		f.sentConfigs = append(f.sentConfigs, msg)
		f.sentTexts = append(f.sentTexts, msg.Text)
	}
	return tgbotapi.Message{}, nil
}

func (f *fakeAPI) Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error) {
	if cb, ok := c.(tgbotapi.CallbackConfig); ok {
		f.callbackTexts = append(f.callbackTexts, cb.Text)
	}
	return &tgbotapi.APIResponse{Ok: true}, nil
}

func newTestHandler(t *testing.T) (*handler, *fakeAPI) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "bot-test.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.InitSchema(context.Background()); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	api := &fakeAPI{}
	return &handler{
		api:     api,
		service: app.NewService(store),
		log:     slog.New(slog.NewTextHandler(testWriter{t: t}, nil)),
		dedupe:  newCallbackDeduper(),
		newAIGenerator: func() (ai.Generator, error) {
			return fakeAIGenerator{}, nil
		},
	}, api
}

type fakeAIGenerator struct{}

func (fakeAIGenerator) GenerateCard(ctx context.Context, req ai.GenerateCardRequest) (ai.GeneratedCard, error) {
	_ = ctx
	if strings.Contains(strings.ToLower(req.Front), "fail") {
		return ai.GeneratedCard{}, fmt.Errorf("provider failed")
	}
	if strings.Contains(strings.ToLower(req.Front), "empty") {
		return ai.GeneratedCard{Back: " "}, nil
	}
	return ai.GeneratedCard{
		Back:          "translated-" + req.Front,
		Pronunciation: "/p/",
		Example:       "d",
		Conjugation:   "",
	}, nil
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (n int, err error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}

func commandMessage(chatID int64, userID int64, text string, command string) *tgbotapi.Message {
	return &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: chatID},
		From: &tgbotapi.User{ID: userID},
		Text: text,
		Entities: []tgbotapi.MessageEntity{{
			Type:   "bot_command",
			Offset: 0,
			Length: len("/" + command),
		}},
	}
}

func TestBotCommandDeckAndCardFlow(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/deck_create EN RU English Basics", "deck_create")}); err != nil {
		t.Fatalf("deck_create: %v", err)
	}
	if len(api.sentTexts) == 0 || !strings.Contains(api.sentTexts[len(api.sentTexts)-1], "Deck created") {
		t.Fatalf("expected deck create confirmation, got %#v", api.sentTexts)
	}

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/card_add 1 | banished | изгнанный | /banished/ | sample", "card_add")}); err != nil {
		t.Fatalf("card_add: %v", err)
	}
	if !strings.Contains(api.sentTexts[len(api.sentTexts)-1], "Card created") {
		t.Fatalf("expected card create confirmation, got %q", api.sentTexts[len(api.sentTexts)-1])
	}

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/next 1", "next")}); err != nil {
		t.Fatalf("next: %v", err)
	}
	last := api.sentConfigs[len(api.sentConfigs)-1]
	if !strings.Contains(last.Text, "<tg-spoiler>") {
		t.Fatalf("expected spoiler formatting in next response, got %q", last.Text)
	}
	if !strings.Contains(last.Text, "Active") {
		t.Fatalf("expected stats line in next response, got %q", last.Text)
	}
}

func TestBotCallbackOwnershipProtection(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()

	deck, err := h.service.CreateDeckForUser(ctx, 1, "basic", "EN", "RU")
	if err != nil {
		t.Fatalf("create deck: %v", err)
	}
	card, err := h.service.AddCardForUser(ctx, 1, deck.ID, "banished", "изгнанный", "", "", "")
	if err != nil {
		t.Fatalf("add card: %v", err)
	}

	cb := &tgbotapi.CallbackQuery{
		ID:      "cb-1",
		Data:    "act=remove;card=1;deck=1",
		From:    &tgbotapi.User{ID: 2},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 10}},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{CallbackQuery: cb}); err != nil {
		t.Fatalf("handle callback: %v", err)
	}
	if len(api.callbackTexts) == 0 || api.callbackTexts[len(api.callbackTexts)-1] != "Card not found" {
		t.Fatalf("expected ownership denial callback, got %#v", api.callbackTexts)
	}

	stored, err := h.service.GetCardByIDForUser(ctx, 1, card.ID)
	if err != nil {
		t.Fatalf("get card after denied callback: %v", err)
	}
	if stored == nil || stored.Status != "active" {
		t.Fatalf("expected card to remain active, got %#v", stored)
	}
}

func TestBotAllowlistDeniesNonAllowedMessageUser(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	h.allow = buildAllowlist([]int64{42})
	ctx := context.Background()

	msg := commandMessage(100, 777, "/help", "help")
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: msg}); err != nil {
		t.Fatalf("handle update: %v", err)
	}
	if len(api.sentTexts) == 0 {
		t.Fatal("expected access denied message")
	}
	if got := api.sentTexts[len(api.sentTexts)-1]; got != "Access denied." {
		t.Fatalf("expected access denied text, got %q", got)
	}
}

func TestBotAllowlistDeniesNonAllowedCallbackUser(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	h.allow = buildAllowlist([]int64{42})
	ctx := context.Background()

	cb := &tgbotapi.CallbackQuery{
		ID:      "cb-deny",
		Data:    "act=remember;card=1;deck=1",
		From:    &tgbotapi.User{ID: 777},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 10}},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{CallbackQuery: cb}); err != nil {
		t.Fatalf("handle callback update: %v", err)
	}
	if len(api.callbackTexts) == 0 {
		t.Fatal("expected callback denial answer")
	}
	if got := api.callbackTexts[len(api.callbackTexts)-1]; got != "Access denied." {
		t.Fatalf("expected access denied callback, got %q", got)
	}
}

func TestBotCardAddBatchAIFlow(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/deck_create EN RU basics", "deck_create")}); err != nil {
		t.Fatalf("deck_create: %v", err)
	}

	payload := "/card_add_batch_ai 1\nbanished\nbanished\nwill fail\nempty back"
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, payload, "card_add_batch_ai")}); err != nil {
		t.Fatalf("card_add_batch_ai: %v", err)
	}
	if len(api.sentTexts) == 0 {
		t.Fatal("expected batch summary response")
	}
	last := api.sentTexts[len(api.sentTexts)-1]
	if !strings.Contains(last, "Batch summary:") {
		t.Fatalf("expected batch summary in response, got %q", last)
	}
	if !strings.Contains(last, "skipped_duplicates=1") {
		t.Fatalf("expected duplicate count in summary, got %q", last)
	}
	if !strings.Contains(last, "failed=2") {
		t.Fatalf("expected failure count in summary, got %q", last)
	}
}
