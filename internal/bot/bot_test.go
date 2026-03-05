package bot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"word-learning/internal/ai"
	"word-learning/internal/app"
	"word-learning/internal/domain"
	"word-learning/internal/storage/sqlite"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type fakeAPI struct {
	sentTexts      []string
	sentConfigs    []tgbotapi.MessageConfig
	sentDocuments  []tgbotapi.DocumentConfig
	callbackTexts  []string
	failSendOnCall int
	sendCallCount  int
}

func (f *fakeAPI) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	f.sendCallCount++
	if f.failSendOnCall > 0 && f.sendCallCount == f.failSendOnCall {
		return tgbotapi.Message{}, fmt.Errorf("send failed")
	}
	if msg, ok := c.(tgbotapi.MessageConfig); ok {
		f.sentConfigs = append(f.sentConfigs, msg)
		f.sentTexts = append(f.sentTexts, msg.Text)
	}
	if doc, ok := c.(tgbotapi.DocumentConfig); ok {
		f.sentDocuments = append(f.sentDocuments, doc)
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
	h, api, _ := newTestHandlerWithStore(t)
	return h, api
}

func newTestHandlerWithStore(t *testing.T) (*handler, *fakeAPI, *sqlite.Store) {
	t.Helper()
	return newTestHandlerWithStoreAndPromptsDir(t, "")
}

func newTestHandlerWithStoreAndPromptsDir(t *testing.T, promptsDir string) (*handler, *fakeAPI, *sqlite.Store) {
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
		api:            api,
		fileDownloader: &fakeFileDownloader{},
		service:        app.NewService(store),
		log:            slog.New(slog.NewTextHandler(testWriter{t: t}, nil)),
		dedupe:         newCallbackDeduper(),
		newAIGenerator: func() (ai.Generator, error) {
			return fakeAIGenerator{}, nil
		},
		promptsDir:  promptsDir,
		randReverse: func() bool { return false },
	}, api, store
}

type fakeFileDownloader struct {
	content []byte
}

func (f *fakeFileDownloader) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	if f.content != nil {
		return f.content, nil
	}
	return []byte(`{"version":1,"deck":{"name":"Imported","language_from":"EN","language_to":"RU"},"cards":[{"front":"a","back":"б","pronunciation":"","example":"","conjugation":""},{"front":"b","back":"в","pronunciation":"","example":"","conjugation":""}]}`), nil
}

type fakeAIGenerator struct{}

func (fakeAIGenerator) GenerateCard(ctx context.Context, req ai.GenerateCardRequest) (ai.GeneratedCard, error) {
	_ = ctx
	if strings.Contains(strings.ToLower(req.Front), "fail") {
		return ai.GeneratedCard{}, fmt.Errorf("provider failed")
	}
	if strings.Contains(strings.ToLower(req.Front), "empty") {
		return ai.GeneratedCard{Front: req.Front, Back: " "}, nil
	}
	return ai.GeneratedCard{
		Front:         req.Front,
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

func plainMessage(chatID int64, userID int64, text string) *tgbotapi.Message {
	return &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: chatID},
		From: &tgbotapi.User{ID: userID},
		Text: text,
	}
}

func TestCancel_ExitsDeckCreateFlow(t *testing.T) {
	t.Parallel()

	promptsDir := filepath.Join(t.TempDir(), "prompts")
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		t.Fatalf("mkdir prompts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "prompt_en-ru.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	h, api, _ := newTestHandlerWithStoreAndPromptsDir(t, promptsDir)
	ctx := context.Background()

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/deck_create", "deck_create")}); err != nil {
		t.Fatalf("deck_create: %v", err)
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: plainMessage(100, 42, "Aborted Deck")}); err != nil {
		t.Fatalf("deck name: %v", err)
	}
	// Now in step 2 (choose language pair). Send /cancel.
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/cancel", "cancel")}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if len(api.sentTexts) == 0 || api.sentTexts[len(api.sentTexts)-1] != "Cancelled." {
		t.Fatalf("expected Cancelled., got %#v", api.sentTexts)
	}
	decks, err := h.service.ListDecksForUser(ctx, 42)
	if err != nil || len(decks) != 0 {
		t.Fatalf("expected no deck after cancel, got %+v err=%v", decks, err)
	}
	// Next message should not be treated as deck create flow
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: plainMessage(100, 42, "random text")}); err != nil {
		t.Fatalf("post-cancel message: %v", err)
	}
	if !strings.Contains(api.sentTexts[len(api.sentTexts)-1], "Use /help") {
		t.Fatalf("expected help hint after cancel, got %q", api.sentTexts[len(api.sentTexts)-1])
	}
}

func TestRenderCardMessage_ReverseShowsBackFirst(t *testing.T) {
	t.Parallel()

	card := domain.Card{Front: "banished", Back: "изгнанный"}
	stats := app.DeckStats{Active: 5, Postponed: 2, Total: 10}
	out := renderCardMessage(card, stats, true)
	if !strings.Contains(out, "<b>изгнанный</b>") {
		t.Errorf("expected back in bold, got %q", out)
	}
	if !strings.Contains(out, "banished") {
		t.Errorf("expected front in spoiler, got %q", out)
	}
	if !strings.Contains(out, "Active 5, postponed 2, total 10") {
		t.Errorf("expected stats line, got %q", out)
	}
}

func TestDeckCreate_OneShotBackwardCompat(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/deck_create EN RU English Basics", "deck_create")}); err != nil {
		t.Fatalf("deck_create one-shot: %v", err)
	}
	if len(api.sentTexts) == 0 || !strings.Contains(api.sentTexts[len(api.sentTexts)-1], "Deck created") {
		t.Fatalf("expected deck create confirmation, got %#v", api.sentTexts)
	}
	decks, err := h.service.ListDecksForUser(ctx, 42)
	if err != nil || len(decks) != 1 || decks[0].Name != "English Basics" || decks[0].LanguageFrom != "EN" || decks[0].LanguageTo != "RU" {
		t.Fatalf("expected deck English Basics EN->RU, got %+v err=%v", decks, err)
	}
}

//nolint:gocyclo // guided flow test has multiple steps
func TestDeckCreate_GuidedFlow(t *testing.T) {
	t.Parallel()

	promptsDir := filepath.Join(t.TempDir(), "prompts")
	if err := os.MkdirAll(promptsDir, 0o755); err != nil {
		t.Fatalf("mkdir prompts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "prompt_en-ru.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	h, api, _ := newTestHandlerWithStoreAndPromptsDir(t, promptsDir)
	ctx := context.Background()

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/deck_create", "deck_create")}); err != nil {
		t.Fatalf("deck_create no args: %v", err)
	}
	if len(api.sentConfigs) == 0 || !strings.Contains(api.sentConfigs[len(api.sentConfigs)-1].Text, "Enter deck name") {
		t.Fatalf("expected Enter deck name prompt, got %#v", api.sentTexts)
	}

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: plainMessage(100, 42, "My Deck")}); err != nil {
		t.Fatalf("deck name message: %v", err)
	}
	last := api.sentConfigs[len(api.sentConfigs)-1]
	if !strings.Contains(last.Text, "Choose language pair") {
		t.Fatalf("expected Choose language pair, got %q", last.Text)
	}
	markup, ok := last.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	if !ok || len(markup.InlineKeyboard) == 0 {
		t.Fatalf("expected language pair keyboard, got %T", last.ReplyMarkup)
	}
	if markup.InlineKeyboard[0][0].CallbackData == nil || !strings.Contains(*markup.InlineKeyboard[0][0].CallbackData, "act=create_deck_pair;from=EN;to=RU") {
		t.Fatalf("expected EN->RU button, got %#v", markup.InlineKeyboard[0][0].CallbackData)
	}

	cb := &tgbotapi.CallbackQuery{
		ID:      "cb-create-pair",
		Data:    "act=create_deck_pair;from=EN;to=RU",
		From:    &tgbotapi.User{ID: 42},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 100}},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{CallbackQuery: cb}); err != nil {
		t.Fatalf("create_deck_pair callback: %v", err)
	}
	if len(api.sentTexts) == 0 || !strings.Contains(api.sentTexts[len(api.sentTexts)-1], "Deck created") {
		t.Fatalf("expected deck created confirmation, got %#v", api.sentTexts)
	}
	decks, err := h.service.ListDecksForUser(ctx, 42)
	if err != nil || len(decks) != 1 || decks[0].Name != "My Deck" || decks[0].LanguageFrom != "EN" || decks[0].LanguageTo != "RU" {
		t.Fatalf("expected deck My Deck EN->RU, got %+v err=%v", decks, err)
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
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/deck_use English Basics", "deck_use")}); err != nil {
		t.Fatalf("deck_use: %v", err)
	}

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/card_add banished | изгнанный | /banished/ | sample", "card_add")}); err != nil {
		t.Fatalf("card_add: %v", err)
	}
	if !strings.Contains(api.sentTexts[len(api.sentTexts)-1], "Card created") {
		t.Fatalf("expected card create confirmation, got %q", api.sentTexts[len(api.sentTexts)-1])
	}

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/next", "next")}); err != nil {
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
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/deck_use basics", "deck_use")}); err != nil {
		t.Fatalf("deck_use: %v", err)
	}

	payload := "/card_add_batch_ai banished\nbanished\nwill fail\nempty back"
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

func TestBotSwitchDeckButtonShowsInlineUseButtons(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	if _, err := h.service.CreateDeckForUser(ctx, 42, "Basics", "EN", "RU"); err != nil {
		t.Fatalf("CreateDeckForUser basics: %v", err)
	}
	if _, err := h.service.CreateDeckForUser(ctx, 42, "Phrasal", "EN", "RU"); err != nil {
		t.Fatalf("CreateDeckForUser phrasal: %v", err)
	}

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: plainMessage(100, 42, startLearningButtonText)}); err != nil {
		t.Fatalf("start learning button: %v", err)
	}
	if len(api.sentConfigs) == 0 {
		t.Fatal("expected menu message")
	}
	last := api.sentConfigs[len(api.sentConfigs)-1]
	if last.Text != "Choose deck:" {
		t.Fatalf("expected choose deck text, got %q", last.Text)
	}
	markup, ok := last.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("expected inline keyboard, got %T", last.ReplyMarkup)
	}
	if len(markup.InlineKeyboard) != 3 {
		t.Fatalf("expected 3 rows (2 decks + Create deck), got %d", len(markup.InlineKeyboard))
	}
	if markup.InlineKeyboard[0][0].CallbackData == nil || !strings.Contains(*markup.InlineKeyboard[0][0].CallbackData, "act=use_deck;deck=") {
		t.Fatalf("expected use_deck payload, got %#v", markup.InlineKeyboard[0][0].CallbackData)
	}
	if markup.InlineKeyboard[2][0].CallbackData == nil || *markup.InlineKeyboard[2][0].CallbackData != "act=create_deck_start" {
		t.Fatalf("expected Create deck button, got %#v", markup.InlineKeyboard[2][0].CallbackData)
	}
}

func TestBotUseDeckCallbackSwitchesActiveAndSendsNextCard(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	deck1, err := h.service.CreateDeckForUser(ctx, 42, "Deck1", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForUser deck1: %v", err)
	}
	deck2, err := h.service.CreateDeckForUser(ctx, 42, "Deck2", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForUser deck2: %v", err)
	}
	if _, err := h.service.DeckUseForUser(ctx, 42, deck1.Name); err != nil {
		t.Fatalf("DeckUseForUser deck1: %v", err)
	}
	if _, err := h.service.AddCardForUser(ctx, 42, deck2.ID, "banished", "изгнанный", "", "", ""); err != nil {
		t.Fatalf("AddCardForUser: %v", err)
	}

	cb := &tgbotapi.CallbackQuery{
		ID:      "cb-use-deck",
		Data:    fmt.Sprintf("act=use_deck;deck=%d", deck2.ID),
		From:    &tgbotapi.User{ID: 42},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 100}},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{CallbackQuery: cb}); err != nil {
		t.Fatalf("handle use_deck callback: %v", err)
	}

	if len(api.callbackTexts) == 0 || api.callbackTexts[len(api.callbackTexts)-1] != "Done" {
		t.Fatalf("expected Done callback answer, got %#v", api.callbackTexts)
	}
	activeDeckConfirmed := false
	for _, sent := range api.sentTexts {
		if strings.Contains(sent, "Active deck: Deck2") {
			activeDeckConfirmed = true
			break
		}
	}
	if !activeDeckConfirmed {
		t.Fatalf("expected active deck confirmation, got %#v", api.sentTexts)
	}
	cardMsg := api.sentConfigs[len(api.sentConfigs)-1]
	if !strings.Contains(cardMsg.Text, "<b>banished</b>") {
		t.Fatalf("expected next card from selected deck, got %q", cardMsg.Text)
	}
}

func TestBotUseDeckCallbackRejectsForeignDeck(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	otherDeck, err := h.service.CreateDeckForUser(ctx, 99, "Other", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForUser other: %v", err)
	}

	cb := &tgbotapi.CallbackQuery{
		ID:      "cb-use-foreign",
		Data:    fmt.Sprintf("act=use_deck;deck=%d", otherDeck.ID),
		From:    &tgbotapi.User{ID: 42},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 100}},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{CallbackQuery: cb}); err != nil {
		t.Fatalf("handle foreign use_deck callback: %v", err)
	}
	if len(api.callbackTexts) == 0 || api.callbackTexts[len(api.callbackTexts)-1] != "Deck not found" {
		t.Fatalf("expected Deck not found callback, got %#v", api.callbackTexts)
	}
}

func TestBotUseDeckCallbackInvalidPayload(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	cb := &tgbotapi.CallbackQuery{
		ID:      "cb-use-invalid",
		Data:    "act=use_deck;deck=bad",
		From:    &tgbotapi.User{ID: 42},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 100}},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{CallbackQuery: cb}); err != nil {
		t.Fatalf("handle invalid use_deck callback: %v", err)
	}
	if len(api.callbackTexts) == 0 || api.callbackTexts[len(api.callbackTexts)-1] != "Invalid action payload" {
		t.Fatalf("expected invalid action callback, got %#v", api.callbackTexts)
	}
}

func TestBotUseDeckCallbackMissingDeckField(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	cb := &tgbotapi.CallbackQuery{
		ID:      "cb-use-missing",
		Data:    "act=use_deck",
		From:    &tgbotapi.User{ID: 42},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 100}},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{CallbackQuery: cb}); err != nil {
		t.Fatalf("handle missing deck payload: %v", err)
	}
	if len(api.callbackTexts) == 0 || api.callbackTexts[len(api.callbackTexts)-1] != "Invalid action payload" {
		t.Fatalf("expected invalid action callback, got %#v", api.callbackTexts)
	}
}

func TestBotUseDeckCallbackNoCardsSendsNoAvailable(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	deck, err := h.service.CreateDeckForUser(ctx, 42, "Empty Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForUser: %v", err)
	}

	cb := &tgbotapi.CallbackQuery{
		ID:      "cb-use-empty",
		Data:    fmt.Sprintf("act=use_deck;deck=%d", deck.ID),
		From:    &tgbotapi.User{ID: 42},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 100}},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{CallbackQuery: cb}); err != nil {
		t.Fatalf("handle use empty deck callback: %v", err)
	}
	if len(api.sentTexts) == 0 || api.sentTexts[len(api.sentTexts)-1] != "No available cards right now." {
		t.Fatalf("expected no cards message, got %#v", api.sentTexts)
	}
}

func TestBotSwitchDeckButtonNoDecks(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: plainMessage(100, 42, startLearningButtonText)}); err != nil {
		t.Fatalf("start learning button: %v", err)
	}
	if len(api.sentTexts) == 0 || api.sentTexts[len(api.sentTexts)-1] != "No decks found." {
		t.Fatalf("expected no decks found text, got %#v", api.sentTexts)
	}
}

func TestBotAddBatchAIButtonShowsModeMenu(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	if _, err := h.service.CreateDeckForUser(ctx, 42, "Batch Deck", "EN", "RU"); err != nil {
		t.Fatalf("CreateDeckForUser: %v", err)
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: plainMessage(100, 42, addBatchAIButtonText)}); err != nil {
		t.Fatalf("add batch ai button: %v", err)
	}
	if len(api.sentConfigs) == 0 {
		t.Fatal("expected mode menu message")
	}
	last := api.sentConfigs[len(api.sentConfigs)-1]
	if last.Text != "Choose deck for batch AI:" {
		t.Fatalf("unexpected mode menu text: %q", last.Text)
	}
	markup, ok := last.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("expected inline keyboard, got %T", last.ReplyMarkup)
	}
	if len(markup.InlineKeyboard) != 1 || len(markup.InlineKeyboard[0]) != 1 {
		t.Fatalf("expected one inline button row, got %#v", markup.InlineKeyboard)
	}
	if markup.InlineKeyboard[0][0].CallbackData == nil || !strings.Contains(*markup.InlineKeyboard[0][0].CallbackData, "act=batch_ai_deck;deck=") {
		t.Fatalf("unexpected callback payload: %#v", markup.InlineKeyboard[0][0].CallbackData)
	}
}

func TestBotSendTextIncludesReplyKeyboard(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/help", "help")}); err != nil {
		t.Fatalf("help command: %v", err)
	}
	if len(api.sentConfigs) == 0 {
		t.Fatal("expected sent message config")
	}
	last := api.sentConfigs[len(api.sentConfigs)-1]
	if _, ok := last.ReplyMarkup.(tgbotapi.ReplyKeyboardMarkup); !ok {
		t.Fatalf("expected reply keyboard in help response, got %T", last.ReplyMarkup)
	}
}

func TestBotBatchAIDeckCallbackConsumesNextMessage(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	deck, err := h.service.CreateDeckForUser(ctx, 42, "Batch Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForUser: %v", err)
	}
	if _, err := h.service.DeckUseByIDForUser(ctx, 42, deck.ID); err != nil {
		t.Fatalf("DeckUseByIDForUser: %v", err)
	}

	cb := &tgbotapi.CallbackQuery{
		ID:      "cb-batch-deck",
		Data:    fmt.Sprintf("act=batch_ai_deck;deck=%d", deck.ID),
		From:    &tgbotapi.User{ID: 42},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 100}},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{CallbackQuery: cb}); err != nil {
		t.Fatalf("batch_ai_deck callback: %v", err)
	}
	if len(api.sentConfigs) == 0 || !strings.Contains(api.sentConfigs[len(api.sentConfigs)-1].Text, "Input mode for deck: Batch Deck (EN->RU)") {
		t.Fatalf("expected input mode prompt, got %#v", api.sentTexts)
	}
	if _, ok := api.sentConfigs[len(api.sentConfigs)-1].ReplyMarkup.(tgbotapi.ForceReply); !ok {
		t.Fatalf("expected ForceReply prompt, got %T", api.sentConfigs[len(api.sentConfigs)-1].ReplyMarkup)
	}

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: plainMessage(100, 42, "banished\ncome up with")}); err != nil {
		t.Fatalf("batch input message: %v", err)
	}
	if len(api.sentTexts) == 0 || !strings.Contains(api.sentTexts[len(api.sentTexts)-1], "Batch summary: total=2") {
		t.Fatalf("expected batch summary after next message, got %#v", api.sentTexts)
	}

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: plainMessage(100, 42, "banished")}); err != nil {
		t.Fatalf("post-batch plain message: %v", err)
	}
	if !strings.Contains(api.sentTexts[len(api.sentTexts)-1], "Use /help to see available commands.") {
		t.Fatalf("expected regular non-batch handling after one-shot consume, got %#v", api.sentTexts[len(api.sentTexts)-1])
	}
}

func TestBotBatchAIDeckCallbackEmptyNextMessageShowsRetryHint(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	deck, err := h.service.CreateDeckForUser(ctx, 42, "Batch Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForUser: %v", err)
	}
	cb := &tgbotapi.CallbackQuery{
		ID:      "cb-batch-empty",
		Data:    fmt.Sprintf("act=batch_ai_deck;deck=%d", deck.ID),
		From:    &tgbotapi.User{ID: 42},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 100}},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{CallbackQuery: cb}); err != nil {
		t.Fatalf("batch_ai_deck callback: %v", err)
	}

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: plainMessage(100, 42, "   ")}); err != nil {
		t.Fatalf("empty batch input message: %v", err)
	}
	if len(api.sentTexts) == 0 || !strings.Contains(api.sentTexts[len(api.sentTexts)-1], "No valid fronts found. Tap Add batch AI and try again.") {
		t.Fatalf("expected retry hint for empty input, got %#v", api.sentTexts)
	}
}

func TestBotBatchAIDeckCallbackInvalidDeckPayload(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	cb := &tgbotapi.CallbackQuery{
		ID:      "cb-batch-invalid",
		Data:    "act=batch_ai_deck;deck=bad",
		From:    &tgbotapi.User{ID: 42},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 100}},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{CallbackQuery: cb}); err != nil {
		t.Fatalf("batch_ai_deck callback: %v", err)
	}
	if len(api.callbackTexts) == 0 || api.callbackTexts[len(api.callbackTexts)-1] != "Invalid action payload" {
		t.Fatalf("expected invalid payload callback, got %#v", api.callbackTexts)
	}
}

func TestBotAddBatchAIButtonNoDecks(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: plainMessage(100, 42, addBatchAIButtonText)}); err != nil {
		t.Fatalf("add batch ai button: %v", err)
	}
	if len(api.sentTexts) == 0 || api.sentTexts[len(api.sentTexts)-1] != "No decks found." {
		t.Fatalf("expected no decks found text, got %#v", api.sentTexts)
	}
}

//nolint:gocyclo // test covers export menu and callback flow
func TestBotDeckExportCommand(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	deck, err := h.service.CreateDeckForUser(ctx, 42, "Export Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForUser: %v", err)
	}
	if _, err := h.service.AddCardForUser(ctx, 42, deck.ID, "hello", "привет", "", "", ""); err != nil {
		t.Fatalf("AddCardForUser: %v", err)
	}

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/deck_export", "deck_export")}); err != nil {
		t.Fatalf("deck_export command: %v", err)
	}
	if len(api.sentConfigs) == 0 {
		t.Fatal("expected deck export menu")
	}
	last := api.sentConfigs[len(api.sentConfigs)-1]
	if last.Text != "Choose deck to export:" {
		t.Fatalf("expected choose deck text, got %q", last.Text)
	}
	markup, ok := last.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	if !ok || len(markup.InlineKeyboard) == 0 {
		t.Fatalf("expected inline keyboard with deck buttons")
	}
	if markup.InlineKeyboard[0][0].CallbackData == nil || !strings.Contains(*markup.InlineKeyboard[0][0].CallbackData, "act=export_deck;deck=") {
		t.Fatalf("expected export_deck payload, got %#v", markup.InlineKeyboard[0][0].CallbackData)
	}

	cb := &tgbotapi.CallbackQuery{
		ID:      "cb-export",
		Data:    fmt.Sprintf("act=export_deck;deck=%d", deck.ID),
		From:    &tgbotapi.User{ID: 42},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 100}},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{CallbackQuery: cb}); err != nil {
		t.Fatalf("export_deck callback: %v", err)
	}
	if len(api.sentDocuments) == 0 {
		t.Fatal("expected document to be sent")
	}
	// Export filename should encode deck name (File is FileBytes with Name)
	if fb, ok := api.sentDocuments[0].File.(tgbotapi.FileBytes); ok && fb.Name != "Export_Deck.json" {
		t.Errorf("expected filename Export_Deck.json, got %q", fb.Name)
	}
}

func TestBotDeckImportDocument(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()

	sendDeckImportAndAssertPrompt(t, h, api, ctx)

	docMsg := &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: 100},
		From: &tgbotapi.User{ID: 42},
		Document: &tgbotapi.Document{
			FileID:   "fake-file-id",
			FileName: "deck.json",
		},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: docMsg}); err != nil {
		t.Fatalf("handle document: %v", err)
	}
	// No suitable deck (EN->RU) exists: user must enter new deck name
	if !strings.Contains(api.sentTexts[len(api.sentTexts)-1], "Enter name for new deck") {
		t.Fatalf("expected prompt for new deck name, got %q", api.sentTexts[len(api.sentTexts)-1])
	}
	// Send deck name as text
	nameMsg := &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 100}, From: &tgbotapi.User{ID: 42}, Text: "Imported"}
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: nameMsg}); err != nil {
		t.Fatalf("handle deck name: %v", err)
	}
	assertImportSuccessMessage(t, api, "Imported", "Import summary")
	assertImportedDeck(t, h.service, ctx, 42, "Imported")
}

func sendDeckImportAndAssertPrompt(t *testing.T, h *handler, api *fakeAPI, ctx context.Context) {
	t.Helper()
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/deck_import", "deck_import")}); err != nil {
		t.Fatalf("deck_import command: %v", err)
	}
	if len(api.sentConfigs) == 0 {
		t.Fatal("expected deck_import prompt message")
	}
	lastConfig := api.sentConfigs[len(api.sentConfigs)-1]
	if !strings.Contains(lastConfig.Text, "Upload") || !strings.Contains(lastConfig.Text, ".json") {
		t.Fatalf("expected upload prompt with .json hint, got %q", lastConfig.Text)
	}
	if _, ok := lastConfig.ReplyMarkup.(tgbotapi.ForceReply); !ok {
		t.Fatalf("expected ForceReply for deck_import prompt, got %T", lastConfig.ReplyMarkup)
	}
}

func assertImportSuccessMessage(t *testing.T, api *fakeAPI, want1, want2 string) {
	t.Helper()
	if len(api.sentTexts) == 0 {
		t.Fatal("expected import reply")
	}
	last := api.sentTexts[len(api.sentTexts)-1]
	if !strings.Contains(last, want1) || !strings.Contains(last, want2) {
		t.Errorf("expected import success message containing %q and %q, got %q", want1, want2, last)
	}
}

func assertImportedDeck(t *testing.T, svc *app.Service, ctx context.Context, userID int64, wantName string) {
	t.Helper()
	decks, err := svc.ListDecksForUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListDecksForUser: %v", err)
	}
	if len(decks) != 1 || decks[0].Name != wantName {
		t.Errorf("expected 1 deck named %q, got %+v", wantName, decks)
	}
}

//nolint:gocyclo // test covers import flow with deck choice
func TestBotDeckImportDocument_WithSuitableDeck(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()

	// Create deck with same language pair as import (EN->RU)
	deck, err := h.service.CreateDeckForUser(ctx, 42, "My EN-RU Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForUser: %v", err)
	}

	sendDeckImportAndAssertPrompt(t, h, api, ctx)

	docMsg := &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: 100},
		From: &tgbotapi.User{ID: 42},
		Document: &tgbotapi.Document{
			FileID:   "fake-file-id",
			FileName: "deck.json",
		},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: docMsg}); err != nil {
		t.Fatalf("handle document: %v", err)
	}
	// Should show deck choice keyboard
	last := api.sentConfigs[len(api.sentConfigs)-1]
	if !strings.Contains(last.Text, "Choose deck") || !strings.Contains(last.Text, "2 cards") {
		t.Fatalf("expected deck choice prompt, got %q", last.Text)
	}
	markup, ok := last.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	if !ok || len(markup.InlineKeyboard) == 0 {
		t.Fatal("expected inline keyboard with deck buttons")
	}
	// Simulate user choosing the deck
	cb := &tgbotapi.CallbackQuery{
		ID:      "cb-import",
		Data:    fmt.Sprintf("act=import_deck;deck=%d", deck.ID),
		From:    &tgbotapi.User{ID: 42},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 100}},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{CallbackQuery: cb}); err != nil {
		t.Fatalf("handle import callback: %v", err)
	}
	if len(api.sentTexts) == 0 {
		t.Fatal("expected success message")
	}
	if !strings.Contains(api.sentTexts[len(api.sentTexts)-1], "Import summary") || !strings.Contains(api.sentTexts[len(api.sentTexts)-1], "created=2") {
		t.Errorf("expected Import summary with created=2, got %q", api.sentTexts[len(api.sentTexts)-1])
	}
	// Cards should be in the chosen deck
	cards, err := h.service.ListCardsInDeck(ctx, deck.ID, "")
	if err != nil {
		t.Fatalf("ListCardsForDeck: %v", err)
	}
	if len(cards) != 2 {
		t.Errorf("expected 2 cards in deck, got %d", len(cards))
	}
}

func TestBotDeckImportDocument_NonJsonRejected(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/deck_import", "deck_import")}); err != nil {
		t.Fatalf("deck_import command: %v", err)
	}

	msg := &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: 100},
		From: &tgbotapi.User{ID: 42},
		Document: &tgbotapi.Document{
			FileID:   "fake",
			FileName: "data.txt",
		},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: msg}); err != nil {
		t.Fatalf("handle document: %v", err)
	}
	if len(api.sentTexts) == 0 {
		t.Fatal("expected reply")
	}
	if !strings.Contains(api.sentTexts[len(api.sentTexts)-1], ".json") {
		t.Errorf("expected .json requirement message, got %q", api.sentTexts[len(api.sentTexts)-1])
	}
}

func TestBotDeckImportDocument_WithoutCommandRejected(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	msg := &tgbotapi.Message{
		Chat: &tgbotapi.Chat{ID: 100},
		From: &tgbotapi.User{ID: 42},
		Document: &tgbotapi.Document{
			FileID:   "fake",
			FileName: "deck.json",
		},
	}

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: msg}); err != nil {
		t.Fatalf("handle document: %v", err)
	}
	if len(api.sentTexts) == 0 {
		t.Fatal("expected reply")
	}
	if !strings.Contains(api.sentTexts[len(api.sentTexts)-1], "/deck_import") {
		t.Errorf("expected /deck_import hint when document sent without command, got %q", api.sentTexts[len(api.sentTexts)-1])
	}
}

func TestBotDeckImportCommandShowsUploadPromptWithForceReply(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/deck_import", "deck_import")}); err != nil {
		t.Fatalf("deck_import command: %v", err)
	}
	if len(api.sentConfigs) == 0 {
		t.Fatal("expected message")
	}
	cfg := api.sentConfigs[len(api.sentConfigs)-1]
	if !strings.Contains(cfg.Text, "Upload") {
		t.Errorf("expected Upload in prompt, got %q", cfg.Text)
	}
	if !strings.Contains(cfg.Text, "deck_export") {
		t.Errorf("expected deck_export hint in prompt, got %q", cfg.Text)
	}
	if _, ok := cfg.ReplyMarkup.(tgbotapi.ForceReply); !ok {
		t.Fatalf("expected ForceReply, got %T", cfg.ReplyMarkup)
	}
}

func TestBotDeckImportTextInsteadOfDocumentShowsRetryHint(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/deck_import", "deck_import")}); err != nil {
		t.Fatalf("deck_import command: %v", err)
	}

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: plainMessage(100, 42, "some text instead of file")}); err != nil {
		t.Fatalf("handle text after deck_import: %v", err)
	}
	if len(api.sentTexts) == 0 {
		t.Fatal("expected reply")
	}
	last := api.sentTexts[len(api.sentTexts)-1]
	if !strings.Contains(last, ".json") || !strings.Contains(last, "/deck_import") {
		t.Errorf("expected retry hint with .json and /deck_import, got %q", last)
	}
}

func TestBotHelpIncludesDeckExportAndImport(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/help", "help")}); err != nil {
		t.Fatalf("help command: %v", err)
	}
	if len(api.sentTexts) == 0 {
		t.Fatal("expected help message")
	}
	help := api.sentTexts[len(api.sentTexts)-1]
	if !strings.Contains(help, "/deck_export") {
		t.Errorf("help should mention /deck_export, got %q", help)
	}
	if !strings.Contains(help, "/deck_import") {
		t.Errorf("help should mention /deck_import, got %q", help)
	}
}

func TestBotUseDeckCallbackReturnsErrorWhenConfirmationSendFails(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	deck, err := h.service.CreateDeckForUser(ctx, 42, "Deck Fail Send", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForUser: %v", err)
	}
	api.failSendOnCall = 1

	cb := &tgbotapi.CallbackQuery{
		ID:      "cb-use-send-fail",
		Data:    fmt.Sprintf("act=use_deck;deck=%d", deck.ID),
		From:    &tgbotapi.User{ID: 42},
		Message: &tgbotapi.Message{Chat: &tgbotapi.Chat{ID: 100}},
	}
	if err := h.handleUpdate(ctx, tgbotapi.Update{CallbackQuery: cb}); err == nil {
		t.Fatal("expected send error from use_deck flow")
	}
	if len(api.callbackTexts) == 0 || api.callbackTexts[len(api.callbackTexts)-1] != "Done" {
		t.Fatalf("expected callback Done before send failure, got %#v", api.callbackTexts)
	}
	current, err := h.service.DeckCurrentForUser(ctx, 42)
	if err != nil {
		t.Fatalf("DeckCurrentForUser: %v", err)
	}
	if current == nil || current.ID != deck.ID {
		t.Fatalf("expected deck switch to persist despite send failure, got %#v", current)
	}
}

func TestBotDeckListCommandShowsInlineUseButtons(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	if _, err := h.service.CreateDeckForUser(ctx, 42, "Basics", "EN", "RU"); err != nil {
		t.Fatalf("CreateDeckForUser basics: %v", err)
	}
	if _, err := h.service.CreateDeckForUser(ctx, 42, "Phrasal", "EN", "RU"); err != nil {
		t.Fatalf("CreateDeckForUser phrasal: %v", err)
	}

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: commandMessage(100, 42, "/deck_list", "deck_list")}); err != nil {
		t.Fatalf("deck_list command: %v", err)
	}
	if len(api.sentConfigs) == 0 {
		t.Fatal("expected deck list response")
	}
	last := api.sentConfigs[len(api.sentConfigs)-1]
	if !strings.Contains(last.Text, "Your decks:") {
		t.Fatalf("expected deck list text, got %q", last.Text)
	}
	markup, ok := last.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("expected inline keyboard, got %T", last.ReplyMarkup)
	}
	if len(markup.InlineKeyboard) != 3 {
		t.Fatalf("expected 3 rows (2 decks + Create deck), got %d", len(markup.InlineKeyboard))
	}
}

func TestBotSwitchDeckButtonCaseInsensitiveAndTrimmed(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()
	if _, err := h.service.CreateDeckForUser(ctx, 42, "Basics", "EN", "RU"); err != nil {
		t.Fatalf("CreateDeckForUser basics: %v", err)
	}

	if err := h.handleUpdate(ctx, tgbotapi.Update{Message: plainMessage(100, 42, "   StArT lEaRnInG   ")}); err != nil {
		t.Fatalf("start learning mixed-case text: %v", err)
	}
	if len(api.sentConfigs) == 0 {
		t.Fatal("expected start learning menu message")
	}
	last := api.sentConfigs[len(api.sentConfigs)-1]
	if last.Text != "Choose deck:" {
		t.Fatalf("expected choose deck text, got %q", last.Text)
	}
}

func TestRunReminderTick_EligibleUserGetsMessage(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()

	h.allow = buildAllowlist([]int64{42})
	if _, err := h.service.CreateDeckForUser(ctx, 42, "Deck", "EN", "RU"); err != nil {
		t.Fatalf("CreateDeckForUser: %v", err)
	}
	if _, err := h.service.DeckUseForUser(ctx, 42, "Deck"); err != nil {
		t.Fatalf("DeckUseForUser: %v", err)
	}
	for i := 0; i < 10; i++ {
		front := fmt.Sprintf("word%d", i)
		if _, err := h.service.AddCardForActiveDeckForUser(ctx, 42, front, "back", "", "", ""); err != nil {
			t.Fatalf("AddCardForActiveDeckForUser: %v", err)
		}
	}

	before := len(api.sentConfigs)
	runReminderTick(ctx, h, 10, 12, time.Now())
	if len(api.sentConfigs) != before+1 {
		t.Fatalf("expected 1 reminder message; got %d new messages", len(api.sentConfigs)-before)
	}
	last := api.sentConfigs[len(api.sentConfigs)-1]
	if last.ChatID != 42 {
		t.Fatalf("expected chat ID 42; got %d", last.ChatID)
	}
	if !strings.Contains(last.Text, "Start learning") {
		t.Fatalf("expected message to contain 'Start learning'; got %q", last.Text)
	}
	if !strings.Contains(last.Text, "cards due for review") {
		t.Fatalf("expected message to contain 'cards due for review'; got %q", last.Text)
	}
}

func TestRunReminderTick_NotEligibleNoMessage(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()

	h.allow = buildAllowlist([]int64{42})
	if _, err := h.service.CreateDeckForUser(ctx, 42, "Deck", "EN", "RU"); err != nil {
		t.Fatalf("CreateDeckForUser: %v", err)
	}
	if _, err := h.service.DeckUseForUser(ctx, 42, "Deck"); err != nil {
		t.Fatalf("DeckUseForUser: %v", err)
	}
	for i := 0; i < 5; i++ {
		front := fmt.Sprintf("w%d", i)
		if _, err := h.service.AddCardForActiveDeckForUser(ctx, 42, front, "b", "", "", ""); err != nil {
			t.Fatalf("AddCardForActiveDeckForUser: %v", err)
		}
	}

	before := len(api.sentConfigs)
	runReminderTick(ctx, h, 10, 12, time.Now())
	if len(api.sentConfigs) != before {
		t.Fatalf("expected no reminder when not eligible; got %d new messages", len(api.sentConfigs)-before)
	}
}

func TestRunReminderTick_MultipleUsersOnlyEligibleGetsMessage(t *testing.T) {
	t.Parallel()

	h, api := newTestHandler(t)
	ctx := context.Background()

	h.allow = buildAllowlist([]int64{42, 43})

	if _, err := h.service.CreateDeckForUser(ctx, 42, "D42", "EN", "RU"); err != nil {
		t.Fatalf("CreateDeckForUser 42: %v", err)
	}
	if _, err := h.service.DeckUseForUser(ctx, 42, "D42"); err != nil {
		t.Fatalf("DeckUseForUser 42: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := h.service.AddCardForActiveDeckForUser(ctx, 42, fmt.Sprintf("a%d", i), "x", "", "", ""); err != nil {
			t.Fatalf("AddCardForActiveDeckForUser 42: %v", err)
		}
	}

	if _, err := h.service.CreateDeckForUser(ctx, 43, "D43", "EN", "RU"); err != nil {
		t.Fatalf("CreateDeckForUser 43: %v", err)
	}
	if _, err := h.service.DeckUseForUser(ctx, 43, "D43"); err != nil {
		t.Fatalf("DeckUseForUser 43: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := h.service.AddCardForActiveDeckForUser(ctx, 43, fmt.Sprintf("b%d", i), "y", "", "", ""); err != nil {
			t.Fatalf("AddCardForActiveDeckForUser 43: %v", err)
		}
	}

	before := len(api.sentConfigs)
	runReminderTick(ctx, h, 10, 12, time.Now())
	if len(api.sentConfigs) != before+1 {
		t.Fatalf("expected 1 reminder; got %d new messages", len(api.sentConfigs)-before)
	}
	last := api.sentConfigs[len(api.sentConfigs)-1]
	if last.ChatID != 42 {
		t.Fatalf("expected reminder to user 42 only; got chat ID %d", last.ChatID)
	}
}

//nolint:gocyclo // test covers 12h-since-review reminder flow with store setup
func TestRunReminderTick_EligibleAfter12hSinceLastReview(t *testing.T) {
	t.Parallel()

	h, api, store := newTestHandlerWithStore(t)
	ctx := context.Background()

	h.allow = buildAllowlist([]int64{42})
	deck, err := h.service.CreateDeckForUser(ctx, 42, "Deck", "EN", "RU")
	if err != nil {
		t.Fatalf("CreateDeckForUser: %v", err)
	}
	if _, err := h.service.DeckUseForUser(ctx, 42, "Deck"); err != nil {
		t.Fatalf("DeckUseForUser: %v", err)
	}
	for i := 0; i < 10; i++ {
		front := fmt.Sprintf("word%d", i)
		if _, err := h.service.AddCardForActiveDeckForUser(ctx, 42, front, "back", "", "", ""); err != nil {
			t.Fatalf("AddCardForActiveDeckForUser: %v", err)
		}
	}

	cards, err := store.ListCards(ctx, deck.ID, nil)
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) < 10 {
		t.Fatalf("expected at least 10 cards, got %d", len(cards))
	}

	now := time.Now().UTC()
	lastReview := now.Add(-13 * time.Hour)
	if ok, err := store.UpdateCardSchedule(ctx, cards[0].ID, now.Add(-time.Hour), 600, 2.5, 0, lastReview); err != nil || !ok {
		t.Fatalf("UpdateCardSchedule: %v", err)
	}

	before := len(api.sentConfigs)
	runReminderTick(ctx, h, 10, 12, now)
	if len(api.sentConfigs) != before+1 {
		t.Fatalf("expected 1 reminder message after 12h since last review; got %d new messages", len(api.sentConfigs)-before)
	}
	last := api.sentConfigs[len(api.sentConfigs)-1]
	if last.ChatID != 42 {
		t.Fatalf("expected chat ID 42; got %d", last.ChatID)
	}
	if !strings.Contains(last.Text, "Start learning") {
		t.Fatalf("expected message to contain 'Start learning'; got %q", last.Text)
	}
	if !strings.Contains(last.Text, "cards due for review") {
		t.Fatalf("expected message to contain 'cards due for review'; got %q", last.Text)
	}
}
