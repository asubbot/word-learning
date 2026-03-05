package bot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"word-learning/internal/ai"
	"word-learning/internal/app"
	"word-learning/internal/domain"
	"word-learning/internal/export"
	"word-learning/internal/storage/sqlite"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type telegramAPI interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
}

const maxImportFileSize = 10 * 1024 * 1024 // 10 MB

type fileDownloader interface {
	DownloadFile(ctx context.Context, fileID string) ([]byte, error)
}

type telegramFileDownloader struct {
	api *tgbotapi.BotAPI
}

func (t *telegramFileDownloader) DownloadFile(ctx context.Context, fileID string) ([]byte, error) {
	url, err := t.api.GetFileDirectURL(fileID)
	if err != nil {
		return nil, fmt.Errorf("get file url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download file: status %d", resp.StatusCode)
	}
	if resp.ContentLength > 0 && resp.ContentLength > maxImportFileSize {
		return nil, fmt.Errorf("file too large (max 10 MB)")
	}
	limited := io.LimitReader(resp.Body, maxImportFileSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("download file: %w", err)
	}
	if len(data) > maxImportFileSize {
		return nil, fmt.Errorf("file too large (max 10 MB)")
	}
	return data, nil
}

type handler struct {
	api                 telegramAPI
	fileDownloader      fileDownloader
	service             *app.Service
	log                 *slog.Logger
	dedupe              *callbackDeduper
	allow               map[int64]struct{}
	newAIGenerator      func() (ai.Generator, error)
	promptsDir          string
	batchAwaitMu        sync.Mutex
	batchAwaitNext      map[int64]batchAwaitState
	importAwaitMu       sync.Mutex
	importAwaitNext     map[int64]importAwaitState
	deckCreateAwaitMu   sync.Mutex
	deckCreateAwaitNext map[int64]deckCreateAwaitState
	randReverse         func() bool // nil = use rand.Intn(2)==1; tests use func() bool { return false }
}

type deckCreateAwaitState struct {
	Step int    // 1 = awaiting name, 2 = awaiting pair
	Name string // set when step 2
}

type batchAwaitState struct {
	deckID int64
}

type importAwaitState struct {
	Exp              *export.DeckExport
	Data             []byte // original JSON for ImportCardsToDeckForUser
	AwaitingDeckName bool   // true = waiting for text (new deck name)
}

type (
	commandHandler        func(context.Context, *tgbotapi.Message, int64) error
	callbackActionHandler func(context.Context, int64, int64) error
)

type callbackTarget struct {
	callbackID string
	chatID     int64
	userID     int64
	cardID     int64
	deckID     int64
	action     string
}

type callbackDeduper struct {
	mu    sync.Mutex
	items map[string]time.Time
}

const (
	startLearningButtonText = "Start learning"
	addBatchAIButtonText    = "Add batch AI"
)

func newCallbackDeduper() *callbackDeduper {
	return &callbackDeduper{items: make(map[string]time.Time)}
}

func (d *callbackDeduper) Seen(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	if _, ok := d.items[id]; ok {
		return true
	}
	d.items[id] = now
	for key, ts := range d.items {
		if now.Sub(ts) > 10*time.Minute {
			delete(d.items, key)
		}
	}
	return false
}

func Run(ctx context.Context, cfg Config, logger *slog.Logger) error {
	store, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	if err := store.InitSchema(ctx); err != nil {
		return err
	}

	botAPI, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		return fmt.Errorf("create telegram bot api: %w", err)
	}

	h := &handler{
		api:            botAPI,
		fileDownloader: &telegramFileDownloader{api: botAPI},
		service:        app.NewService(store),
		log:            logger,
		dedupe:         newCallbackDeduper(),
		allow:          buildAllowlist(cfg.AllowedUserIDs),
		newAIGenerator: ai.NewGeneratorFromEnv,
		promptsDir:     cfg.PromptsDir,
		randReverse:    func() bool { return rand.Intn(2) == 1 },
	}

	go runReminderLoop(ctx, h, cfg.ReminderIntervalMin, cfg.ReminderMinOverdue, cfg.ReminderMinHoursSinceReview)

	updateCfg := tgbotapi.NewUpdate(0)
	updateCfg.Timeout = cfg.PollingTimeout
	updates := botAPI.GetUpdatesChan(updateCfg)
	logger.Info("telegram bot started", "bot_username", botAPI.Self.UserName, "timeout_seconds", cfg.PollingTimeout)

	for {
		select {
		case <-ctx.Done():
			logger.Info("telegram bot stopped")
			return nil
		case update := <-updates:
			if err := h.handleUpdate(ctx, update); err != nil {
				logger.Error("handle update", "error", err)
			}
		}
	}
}

func (h *handler) handleUpdate(ctx context.Context, update tgbotapi.Update) error {
	if update.Message != nil {
		return h.handleMessageUpdate(ctx, update)
	}
	if update.CallbackQuery != nil {
		return h.handleCallbackUpdate(ctx, update)
	}
	return nil
}

func (h *handler) handleMessageUpdate(ctx context.Context, update tgbotapi.Update) error {
	msg := update.Message
	if msg.From == nil {
		return nil
	}
	if msg.IsCommand() && strings.ToLower(msg.Command()) == "whoami" {
		return h.handleWhoAmICommand(ctx, msg, msg.From.ID)
	}
	if !h.isAllowed(msg.From.ID) {
		h.log.Warn("deny message from non-allowlisted user", "user_id", msg.From.ID)
		return h.sendText(msg.Chat.ID, "Access denied.")
	}
	if msg.IsCommand() && strings.ToLower(msg.Command()) == "cancel" {
		return h.handleCancelCommand(ctx, msg.Chat.ID, msg.From.ID)
	}
	if state, ok := h.consumeAwaitBatchAI(msg.From.ID); ok {
		return h.handleBatchAIInputMessage(ctx, msg, msg.From.ID, state)
	}
	if impState, ok := h.getImportState(msg.From.ID); ok {
		return h.handleImportMessage(ctx, msg, impState)
	}
	if deckState, ok := h.getDeckCreateState(msg.From.ID); ok {
		return h.handleDeckCreateMessage(ctx, msg, msg.From.ID, deckState)
	}
	if msg.IsCommand() {
		return h.handleCommand(ctx, msg)
	}
	if msg.Document != nil {
		return h.sendText(msg.Chat.ID, "Use /deck_import to import a deck.")
	}
	return h.handleTextMessage(ctx, msg)
}

func (h *handler) handleCallbackUpdate(ctx context.Context, update tgbotapi.Update) error {
	cb := update.CallbackQuery
	if cb.From == nil {
		return nil
	}
	if !h.isAllowed(cb.From.ID) {
		h.log.Warn("deny callback from non-allowlisted user", "user_id", cb.From.ID)
		return h.notifyAndReturn(cb.ID, "Access denied.", nil)
	}
	return h.handleCallback(ctx, cb)
}

func (h *handler) handleCommand(ctx context.Context, msg *tgbotapi.Message) error {
	command := strings.ToLower(msg.Command())
	userID := msg.From.ID
	handlers := map[string]commandHandler{
		"start":             h.handleStartCommand,
		"help":              h.handleHelpCommand,
		"whoami":            h.handleWhoAmICommand,
		"deck_create":       h.handleDeckCreateCommand,
		"deck_list":         h.handleDeckListCommand,
		"deck_use":          h.handleDeckUseCommand,
		"deck_current":      h.handleDeckCurrentCommand,
		"deck_export":       h.handleDeckExportCommand,
		"deck_import":       h.handleDeckImportCommand,
		"card_add":          h.handleCardAddCommand,
		"card_add_batch_ai": h.handleCardAddBatchAICommand,
		"next":              h.handleNextCommand,
	}
	handlerFn, ok := handlers[command]
	if !ok {
		return h.sendText(msg.Chat.ID, "Unknown command. Use /help.")
	}
	return handlerFn(ctx, msg, userID)
}

//nolint:gocyclo // callback routing has many action branches
func (h *handler) handleCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) error {
	if cb == nil || cb.Message == nil || cb.From == nil {
		return nil
	}
	if h.dedupe.Seen(cb.ID) {
		return h.answerCallback(cb.ID, "Already processed.")
	}
	fields := parseKVPayload(cb.Data, ";", "=")
	action := strings.TrimSpace(fields["act"])
	if action == "use_deck" {
		return h.handleUseDeckCallback(ctx, cb, fields)
	}
	if action == "batch_ai_deck" {
		return h.handleBatchAIDeckCallback(ctx, cb, fields)
	}
	if action == "export_deck" {
		return h.handleExportDeckCallback(ctx, cb, fields)
	}
	if action == "import_deck" {
		return h.handleImportDeckCallback(ctx, cb, fields)
	}
	if action == "create_deck_pair" {
		return h.handleCreateDeckPairCallback(ctx, cb, fields)
	}
	if action == "create_deck_start" {
		return h.handleCreateDeckStartCallback(ctx, cb)
	}

	target, ok := h.resolveCallbackTarget(ctx, cb)
	if !ok {
		return nil
	}
	if err := h.executeCallbackAction(ctx, target); err != nil {
		return h.notifyAndReturn(target.callbackID, "Action failed", err)
	}
	if err := h.answerCallback(target.callbackID, "Done"); err != nil {
		h.log.Warn("answer callback", "error", err)
	}
	return h.sendNextCard(ctx, target.chatID, target.userID)
}

func (h *handler) handleUseDeckCallback(ctx context.Context, cb *tgbotapi.CallbackQuery, fields map[string]string) error {
	deckRaw := strings.TrimSpace(fields["deck"])
	deckID, err := parsePositiveInt(deckRaw, "invalid deck id")
	if err != nil {
		return h.notifyAndReturn(cb.ID, "Invalid action payload", err)
	}
	deck, err := h.service.DeckUseByIDForUser(ctx, cb.From.ID, deckID)
	if err != nil {
		return h.notifyAndReturn(cb.ID, "Deck not found", err)
	}
	if err := h.answerCallback(cb.ID, "Done"); err != nil {
		h.log.Warn("answer callback", "error", err)
	}
	if err := h.sendText(cb.Message.Chat.ID, fmt.Sprintf("Active deck: %s (%s->%s)", deck.Name, deck.LanguageFrom, deck.LanguageTo)); err != nil {
		return err
	}
	return h.sendNextCard(ctx, cb.Message.Chat.ID, cb.From.ID)
}

func (h *handler) handleTextMessage(ctx context.Context, msg *tgbotapi.Message) error {
	text := strings.TrimSpace(msg.Text)
	switch {
	case strings.EqualFold(text, startLearningButtonText):
		return h.sendSwitchDeckMenu(ctx, msg.Chat.ID, msg.From.ID)
	case strings.EqualFold(text, addBatchAIButtonText):
		return h.sendBatchAIDeckMenu(ctx, msg.Chat.ID, msg.From.ID)
	default:
		return h.sendText(msg.Chat.ID, "Use /help to see available commands.")
	}
}

func (h *handler) handleBatchAIDeckCallback(ctx context.Context, cb *tgbotapi.CallbackQuery, fields map[string]string) error {
	if cb == nil || cb.From == nil || cb.Message == nil {
		return nil
	}
	deckRaw := strings.TrimSpace(fields["deck"])
	deckID, err := parsePositiveInt(deckRaw, "invalid deck id")
	if err != nil {
		return h.notifyAndReturn(cb.ID, "Invalid action payload", err)
	}
	deck, found, err := h.findDeckForUserByID(ctx, cb.From.ID, deckID)
	if err != nil {
		return h.sendText(cb.Message.Chat.ID, "Failed to list decks: "+userFriendlyError(err))
	}
	if !found {
		return h.notifyAndReturn(cb.ID, "Deck not found", nil)
	}
	if err := h.answerCallback(cb.ID, "Done"); err != nil {
		h.log.Warn("answer callback", "error", err)
	}
	h.setAwaitBatchAI(cb.From.ID, batchAwaitState{deckID: deckID})
	msg := tgbotapi.NewMessage(cb.Message.Chat.ID, fmt.Sprintf("Input mode for deck: %s (%s->%s)\nSend newline-separated fronts in your NEXT message.\n\nExample:\nbanished\ncome up with", deck.Name, deck.LanguageFrom, deck.LanguageTo))
	msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}
	_, err = h.sendWithRetry(msg)
	return err
}

func (h *handler) resolveCallbackTarget(ctx context.Context, cb *tgbotapi.CallbackQuery) (callbackTarget, bool) {
	action, cardID, deckID, err := parseCallbackData(cb.Data)
	if err != nil {
		_ = h.answerCallback(cb.ID, "Invalid action payload")
		return callbackTarget{}, false
	}
	card, err := h.service.GetCardByIDForUser(ctx, cb.From.ID, cardID)
	if err != nil {
		_ = h.answerCallback(cb.ID, "Card not found")
		return callbackTarget{}, false
	}
	if card.DeckID != deckID {
		_ = h.answerCallback(cb.ID, "Card/deck mismatch")
		return callbackTarget{}, false
	}
	return callbackTarget{
		callbackID: cb.ID,
		chatID:     cb.Message.Chat.ID,
		userID:     cb.From.ID,
		cardID:     cardID,
		deckID:     deckID,
		action:     action,
	}, true
}

func (h *handler) executeCallbackAction(ctx context.Context, target callbackTarget) error {
	actions := map[string]callbackActionHandler{
		"remember":      h.service.RememberCardForUser,
		"dont_remember": h.service.DontRememberCardForUser,
		"remove":        h.service.RemoveCardForUser,
	}
	actionFn, ok := actions[target.action]
	if !ok {
		return fmt.Errorf("unknown action")
	}
	return actionFn(ctx, target.userID, target.cardID)
}

func parseCallbackData(data string) (string, int64, int64, error) {
	fields := parseKVPayload(data, ";", "=")
	action := strings.TrimSpace(fields["act"])
	cardRaw := strings.TrimSpace(fields["card"])
	deckRaw := strings.TrimSpace(fields["deck"])
	if action == "" || cardRaw == "" || deckRaw == "" {
		return "", 0, 0, fmt.Errorf("missing callback fields")
	}
	cardID, err := parsePositiveInt(cardRaw, "invalid card id")
	if err != nil {
		return "", 0, 0, err
	}
	deckID, err := parsePositiveInt(deckRaw, "invalid deck id")
	if err != nil {
		return "", 0, 0, err
	}
	return action, cardID, deckID, nil
}

func (h *handler) handleStartCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	_ = ctx
	_ = userID
	return h.sendText(msg.Chat.ID, helpMessage())
}

func (h *handler) handleHelpCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	return h.handleStartCommand(ctx, msg, userID)
}

func (h *handler) handleWhoAmICommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	_ = ctx
	return h.sendText(msg.Chat.ID, fmt.Sprintf("Your Telegram user ID: %d", userID))
}

func (h *handler) handleCancelCommand(ctx context.Context, chatID int64, userID int64) error {
	_ = ctx
	h.clearDeckCreateState(userID)
	h.clearImportState(userID)
	h.clearAwaitBatchAI(userID)
	return h.sendText(chatID, "Cancelled.")
}

func (h *handler) clearAwaitBatchAI(userID int64) {
	h.batchAwaitMu.Lock()
	defer h.batchAwaitMu.Unlock()
	if h.batchAwaitNext != nil {
		delete(h.batchAwaitNext, userID)
	}
}

func (h *handler) handleDeckCreateCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	args := strings.TrimSpace(msg.CommandArguments())
	if args != "" {
		name, langFrom, langTo, err := parseDeckCreateArgs(args)
		if err != nil {
			return h.sendText(msg.Chat.ID, err.Error())
		}
		deck, err := h.service.CreateDeckForUser(ctx, userID, name, langFrom, langTo)
		if err != nil {
			return h.sendText(msg.Chat.ID, "Failed to create deck: "+userFriendlyError(err))
		}
		return h.sendText(msg.Chat.ID, fmt.Sprintf("Deck created: id=%d name=%q pair=%s->%s", deck.ID, deck.Name, deck.LanguageFrom, deck.LanguageTo))
	}
	h.setDeckCreateState(userID, deckCreateAwaitState{Step: 1})
	reply := tgbotapi.NewMessage(msg.Chat.ID, "Enter deck name:")
	reply.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}
	_, err := h.sendWithRetry(reply)
	return err
}

func (h *handler) handleDeckCreateMessage(ctx context.Context, msg *tgbotapi.Message, userID int64, state deckCreateAwaitState) error {
	chatID := msg.Chat.ID
	if state.Step == 1 {
		name := strings.TrimSpace(msg.Text)
		if name == "" {
			return h.sendText(chatID, "Deck name cannot be empty. Enter deck name:")
		}
		pairs := ai.ListAvailableLanguagePairs(h.promptsDir)
		if len(pairs) == 0 {
			h.clearDeckCreateState(userID)
			return h.sendText(chatID, "No supported language pairs found. Use /deck_create <from> <to> <name> to create a deck manually.")
		}
		h.setDeckCreateState(userID, deckCreateAwaitState{Step: 2, Name: name})
		reply := tgbotapi.NewMessage(chatID, "Choose language pair:")
		reply.ReplyMarkup = languagePairKeyboard(pairs)
		_, err := h.sendWithRetry(reply)
		return err
	}
	h.clearDeckCreateState(userID)
	return h.sendText(chatID, "Use the buttons above.")
}

func (h *handler) handleCreateDeckPairCallback(ctx context.Context, cb *tgbotapi.CallbackQuery, fields map[string]string) error {
	from := strings.ToUpper(strings.TrimSpace(fields["from"]))
	to := strings.ToUpper(strings.TrimSpace(fields["to"]))
	if from == "" || to == "" {
		return h.notifyAndReturn(cb.ID, "Invalid action payload", nil)
	}
	state, ok := h.getDeckCreateState(cb.From.ID)
	if !ok || state.Step != 2 {
		_ = h.answerCallback(cb.ID, "Session expired. Use /deck_create.")
		return h.sendText(cb.Message.Chat.ID, "Session expired. Use /deck_create to start again.")
	}
	deck, err := h.service.CreateDeckForUser(ctx, cb.From.ID, state.Name, from, to)
	h.clearDeckCreateState(cb.From.ID)
	if err != nil {
		_ = h.answerCallback(cb.ID, "Failed")
		return h.sendText(cb.Message.Chat.ID, "Failed to create deck: "+userFriendlyError(err))
	}
	if err := h.answerCallback(cb.ID, "Done"); err != nil {
		h.log.Warn("answer callback", "error", err)
	}
	return h.sendText(cb.Message.Chat.ID, fmt.Sprintf("Deck created: id=%d name=%q pair=%s->%s", deck.ID, deck.Name, deck.LanguageFrom, deck.LanguageTo))
}

func (h *handler) handleCreateDeckStartCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) error {
	h.setDeckCreateState(cb.From.ID, deckCreateAwaitState{Step: 1})
	if err := h.answerCallback(cb.ID, "Done"); err != nil {
		h.log.Warn("answer callback", "error", err)
	}
	reply := tgbotapi.NewMessage(cb.Message.Chat.ID, "Enter deck name:")
	reply.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}
	_, err := h.sendWithRetry(reply)
	return err
}

func languagePairKeyboard(pairs []ai.LanguagePair) tgbotapi.InlineKeyboardMarkup {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(pairs))
	for _, p := range pairs {
		label := fmt.Sprintf("%s->%s", p.From, p.To)
		payload := fmt.Sprintf("act=create_deck_pair;from=%s;to=%s", p.From, p.To)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, payload)))
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (h *handler) handleDeckListCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	decks, err := h.service.ListDecksForUser(ctx, userID)
	if err != nil {
		return h.sendText(msg.Chat.ID, "Failed to list decks: "+userFriendlyError(err))
	}
	if len(decks) == 0 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "No decks found.")
		reply.ReplyMarkup = createDeckOnlyKeyboard()
		_, err = h.sendWithRetry(reply)
		return err
	}
	var b strings.Builder
	b.WriteString("Your decks:\n")
	for _, d := range decks {
		fmt.Fprintf(&b, "- #%d %s (%s->%s)\n", d.ID, d.Name, d.LanguageFrom, d.LanguageTo)
	}
	reply := tgbotapi.NewMessage(msg.Chat.ID, strings.TrimSpace(b.String()))
	reply.ReplyMarkup = deckSwitchKeyboard(decks)
	_, err = h.sendWithRetry(reply)
	return err
}

func (h *handler) handleDeckUseCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	name := strings.TrimSpace(msg.CommandArguments())
	if name == "" {
		return h.sendText(msg.Chat.ID, "usage: /deck_use <name...>")
	}
	result, err := h.service.DeckUseForUser(ctx, userID, name)
	if err != nil {
		if errors.Is(err, app.ErrDeckNameAmbiguous) {
			if len(result.Candidates) == 0 {
				return h.sendText(msg.Chat.ID, "Deck name is ambiguous. Please use exact deck name.")
			}
			var b strings.Builder
			b.WriteString("Deck name is ambiguous. Candidates:\n")
			for _, d := range result.Candidates {
				fmt.Fprintf(&b, "- %s (%s->%s)\n", d.Name, d.LanguageFrom, d.LanguageTo)
			}
			return h.sendText(msg.Chat.ID, strings.TrimSpace(b.String()))
		}
		return h.sendText(msg.Chat.ID, "Failed to set active deck: "+userFriendlyError(err))
	}
	if result.Deck == nil {
		return h.sendText(msg.Chat.ID, "Failed to set active deck.")
	}
	return h.sendText(msg.Chat.ID, fmt.Sprintf("Active deck: %s (%s->%s)", result.Deck.Name, result.Deck.LanguageFrom, result.Deck.LanguageTo))
}

func (h *handler) handleDeckCurrentCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	deck, err := h.service.DeckCurrentForUser(ctx, userID)
	if err != nil {
		return h.sendText(msg.Chat.ID, "Failed to resolve active deck: "+userFriendlyError(err))
	}
	if deck == nil {
		return h.sendText(msg.Chat.ID, "Active deck is not set. Use /deck_use <name...>.")
	}
	return h.sendText(msg.Chat.ID, fmt.Sprintf("Active deck: %s (%s->%s)", deck.Name, deck.LanguageFrom, deck.LanguageTo))
}

func (h *handler) handleDeckExportCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	return h.sendExportDeckMenu(ctx, msg.Chat.ID, userID)
}

func (h *handler) handleDeckImportCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	h.setAwaitImport(userID)
	reply := tgbotapi.NewMessage(msg.Chat.ID, "Upload a .json file to import a deck.\n\nUse /deck_export to export a deck first, or get a .json file from someone else.")
	reply.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}
	_, err := h.sendWithRetry(reply)
	return err
}

func (h *handler) setAwaitImport(userID int64) {
	h.setImportState(userID, importAwaitState{})
}

func (h *handler) getImportState(userID int64) (importAwaitState, bool) {
	h.importAwaitMu.Lock()
	defer h.importAwaitMu.Unlock()
	if h.importAwaitNext == nil {
		return importAwaitState{}, false
	}
	state, ok := h.importAwaitNext[userID]
	return state, ok
}

func (h *handler) setImportState(userID int64, state importAwaitState) {
	h.importAwaitMu.Lock()
	defer h.importAwaitMu.Unlock()
	if h.importAwaitNext == nil {
		h.importAwaitNext = make(map[int64]importAwaitState)
	}
	h.importAwaitNext[userID] = state
}

func (h *handler) clearImportState(userID int64) {
	h.importAwaitMu.Lock()
	defer h.importAwaitMu.Unlock()
	if h.importAwaitNext != nil {
		delete(h.importAwaitNext, userID)
	}
}

func (h *handler) getDeckCreateState(userID int64) (deckCreateAwaitState, bool) {
	h.deckCreateAwaitMu.Lock()
	defer h.deckCreateAwaitMu.Unlock()
	if h.deckCreateAwaitNext == nil {
		return deckCreateAwaitState{}, false
	}
	state, ok := h.deckCreateAwaitNext[userID]
	return state, ok
}

func (h *handler) setDeckCreateState(userID int64, state deckCreateAwaitState) {
	h.deckCreateAwaitMu.Lock()
	defer h.deckCreateAwaitMu.Unlock()
	if h.deckCreateAwaitNext == nil {
		h.deckCreateAwaitNext = make(map[int64]deckCreateAwaitState)
	}
	h.deckCreateAwaitNext[userID] = state
}

func (h *handler) clearDeckCreateState(userID int64) {
	h.deckCreateAwaitMu.Lock()
	defer h.deckCreateAwaitMu.Unlock()
	if h.deckCreateAwaitNext != nil {
		delete(h.deckCreateAwaitNext, userID)
	}
}

func (h *handler) sendExportDeckMenu(ctx context.Context, chatID int64, userID int64) error {
	decks, err := h.service.ListDecksForUser(ctx, userID)
	if err != nil {
		return h.sendText(chatID, "Failed to list decks: "+userFriendlyError(err))
	}
	if len(decks) == 0 {
		return h.sendText(chatID, "No decks found.")
	}
	msg := tgbotapi.NewMessage(chatID, "Choose deck to export:")
	msg.ReplyMarkup = exportDeckKeyboard(decks)
	_, err = h.sendWithRetry(msg)
	return err
}

func (h *handler) handleExportDeckCallback(ctx context.Context, cb *tgbotapi.CallbackQuery, fields map[string]string) error {
	deckRaw := strings.TrimSpace(fields["deck"])
	deckID, err := parsePositiveInt(deckRaw, "invalid deck id")
	if err != nil {
		return h.notifyAndReturn(cb.ID, "Invalid action payload", err)
	}
	deck, found, err := h.findDeckForUserByID(ctx, cb.From.ID, deckID)
	if err != nil || !found {
		return h.notifyAndReturn(cb.ID, "Deck not found", err)
	}
	data, err := h.service.ExportDeckForUser(ctx, cb.From.ID, deck.ID)
	if err != nil {
		_ = h.answerCallback(cb.ID, "Export failed")
		return h.sendText(cb.Message.Chat.ID, "Failed to export: "+userFriendlyError(err))
	}
	_ = h.answerCallback(cb.ID, "Done")
	return h.sendDocument(cb.Message.Chat.ID, export.ExportFilename(deck.Name), data)
}

func (h *handler) sendDocument(chatID int64, filename string, data []byte) error {
	doc := tgbotapi.NewDocument(chatID, tgbotapi.FileBytes{Name: filename, Bytes: data})
	_, err := h.sendWithRetry(doc)
	return err
}

//nolint:gocyclo // import flow has multiple branches for document/text/callback
func (h *handler) handleImportMessage(ctx context.Context, msg *tgbotapi.Message, state importAwaitState) error {
	userID := msg.From.ID
	chatID := msg.Chat.ID

	// Awaiting document (initial state from /deck_import)
	if state.Exp == nil {
		if msg.Document == nil {
			return h.sendText(chatID, "Please send a .json file. Use /deck_import to try again.")
		}
		fname := strings.ToLower(strings.TrimSpace(msg.Document.FileName))
		if !strings.HasSuffix(fname, ".json") {
			return h.sendText(chatID, "Please send a .json file. Use /deck_import to try again.")
		}
		if h.fileDownloader == nil {
			return h.sendText(chatID, "File download is not available.")
		}
		data, err := h.fileDownloader.DownloadFile(ctx, msg.Document.FileID)
		if err != nil {
			return h.sendText(chatID, "Failed to download file: "+userFriendlyError(err))
		}
		exp, err := export.UnmarshalExport(data)
		if err != nil {
			return h.sendText(chatID, "Failed to parse file: "+userFriendlyError(err))
		}
		normalizedFrom := strings.ToUpper(strings.TrimSpace(exp.Deck.LanguageFrom))
		normalizedTo := strings.ToUpper(strings.TrimSpace(exp.Deck.LanguageTo))
		decks, err := h.service.ListDecksForUser(ctx, userID)
		if err != nil {
			return h.sendText(chatID, "Failed to list decks: "+userFriendlyError(err))
		}
		var suitableDecks []domain.Deck
		for _, d := range decks {
			if d.LanguageFrom == normalizedFrom && d.LanguageTo == normalizedTo {
				suitableDecks = append(suitableDecks, d)
			}
		}
		pair := fmt.Sprintf("%s->%s", normalizedFrom, normalizedTo)
		if len(suitableDecks) > 0 {
			text := fmt.Sprintf("Choose deck to add %d cards (%s):", len(exp.Cards), pair)
			keyboard := importDeckKeyboard(suitableDecks)
			msg := tgbotapi.NewMessage(chatID, text)
			msg.ReplyMarkup = keyboard
			if _, err := h.sendWithRetry(msg); err != nil {
				return err
			}
			h.setImportState(userID, importAwaitState{Exp: exp, Data: data, AwaitingDeckName: false})
		} else {
			reply := tgbotapi.NewMessage(chatID, fmt.Sprintf("No deck with %s. Enter name for new deck:", pair))
			reply.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true, Selective: true}
			if _, err := h.sendWithRetry(reply); err != nil {
				return err
			}
			h.setImportState(userID, importAwaitState{Exp: exp, Data: data, AwaitingDeckName: true})
		}
		return nil
	}

	// Awaiting deck choice (user should use inline buttons)
	if !state.AwaitingDeckName {
		return h.sendText(chatID, "Use the buttons above to choose a deck.")
	}

	// Awaiting deck name (text input)
	if msg.Document != nil {
		return h.sendText(chatID, "Enter the name for the new deck above.")
	}
	name := strings.TrimSpace(msg.Text)
	if name == "" {
		return h.sendText(chatID, "Deck name must not be empty.")
	}
	deck, err := h.service.CreateDeckForUser(ctx, userID, name, state.Exp.Deck.LanguageFrom, state.Exp.Deck.LanguageTo)
	if err != nil {
		return h.sendText(chatID, "Failed to create deck: "+userFriendlyError(err))
	}
	report, err := h.service.ImportCardsToDeckForUser(ctx, userID, deck.ID, state.Data)
	if err != nil {
		return h.sendText(chatID, "Failed to add cards: "+userFriendlyError(err))
	}
	h.clearImportState(userID)
	return h.sendImportSummary(chatID, deck.Name, deck.LanguageFrom, deck.LanguageTo, report)
}

func (h *handler) handleImportDeckCallback(ctx context.Context, cb *tgbotapi.CallbackQuery, fields map[string]string) error {
	deckRaw := strings.TrimSpace(fields["deck"])
	deckID, err := parsePositiveInt(deckRaw, "invalid deck id")
	if err != nil {
		return h.notifyAndReturn(cb.ID, "Invalid action payload", err)
	}
	state, ok := h.getImportState(cb.From.ID)
	if !ok || state.Exp == nil || state.Data == nil {
		return h.notifyAndReturn(cb.ID, "Import session expired. Use /deck_import to try again.", nil)
	}
	report, err := h.service.ImportCardsToDeckForUser(ctx, cb.From.ID, deckID, state.Data)
	if err != nil {
		_ = h.answerCallback(cb.ID, "Failed")
		return h.sendText(cb.Message.Chat.ID, "Failed to add cards: "+userFriendlyError(err))
	}
	h.clearImportState(cb.From.ID)
	_ = h.answerCallback(cb.ID, "Done")
	deck, err := h.service.GetDeckByID(ctx, deckID)
	if err != nil || deck == nil {
		return h.sendImportSummary(cb.Message.Chat.ID, "", "", "", report)
	}
	return h.sendImportSummary(cb.Message.Chat.ID, deck.Name, deck.LanguageFrom, deck.LanguageTo, report)
}

func (h *handler) sendImportSummary(chatID int64, deckName, langFrom, langTo string, report app.ImportReport) error {
	var b strings.Builder
	if deckName != "" {
		fmt.Fprintf(&b, "Added cards to %q (%s->%s).\n", deckName, langFrom, langTo)
	}
	fmt.Fprintf(&b, "Import summary: total=%d created=%d skipped_duplicates=%d failed=%d",
		report.Total, report.Created, report.SkippedDuplicates, report.Failed)
	return h.sendText(chatID, b.String())
}

func importDeckKeyboard(decks []domain.Deck) tgbotapi.InlineKeyboardMarkup {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(decks))
	for _, d := range decks {
		label := fmt.Sprintf("%s (%s->%s)", d.Name, d.LanguageFrom, d.LanguageTo)
		payload := fmt.Sprintf("act=import_deck;deck=%d", d.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, payload)))
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (h *handler) handleCardAddCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	front, back, pronunciation, example, conjugation, err := parseCardAddArgs(msg.CommandArguments())
	if err != nil {
		return h.sendText(msg.Chat.ID, err.Error())
	}
	card, err := h.service.AddCardForActiveDeckForUser(ctx, userID, front, back, pronunciation, example, conjugation)
	if err != nil {
		if errors.Is(err, app.ErrActiveDeckNotSet) {
			return h.sendText(msg.Chat.ID, "Active deck is not set. Use /deck_use <name...>.")
		}
		return h.sendText(msg.Chat.ID, "Failed to add card: "+userFriendlyError(err))
	}
	return h.sendText(msg.Chat.ID, fmt.Sprintf("Card created: id=%d deck=%d", card.ID, card.DeckID))
}

func (h *handler) handleCardAddBatchAICommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	lines, err := parseCardAddBatchAIArgs(msg.CommandArguments())
	if err != nil {
		return h.sendText(msg.Chat.ID, err.Error())
	}
	return h.runBatchAIGeneration(ctx, msg.Chat.ID, userID, lines)
}

func (h *handler) handleBatchAIInputMessage(ctx context.Context, msg *tgbotapi.Message, userID int64, state batchAwaitState) error {
	lines, err := parseCardAddBatchAIArgs(msg.Text)
	if err != nil {
		return h.sendText(msg.Chat.ID, "No valid fronts found. Tap Add batch AI and try again.")
	}
	return h.runBatchAIGenerationForDeck(ctx, msg.Chat.ID, userID, state.deckID, lines)
}

func (h *handler) runBatchAIGeneration(ctx context.Context, chatID int64, userID int64, lines []string) error {
	deck, err := h.service.ResolveActiveDeckForUser(ctx, userID)
	if err != nil {
		if errors.Is(err, app.ErrActiveDeckNotSet) {
			return h.sendText(chatID, "Active deck is not set. Use /deck_use <name...>.")
		}
		return h.sendText(chatID, "Failed to resolve active deck: "+userFriendlyError(err))
	}
	return h.runBatchAIGenerationForDeck(ctx, chatID, userID, deck.ID, lines)
}

func (h *handler) runBatchAIGenerationForDeck(ctx context.Context, chatID int64, userID int64, deckID int64, lines []string) error {
	generator, err := h.newAIGenerator()
	if err != nil {
		return h.sendText(chatID, "Failed to configure AI: "+userFriendlyError(err))
	}
	report, err := h.service.AddCardsBatchAIForUser(ctx, userID, generator, app.BatchAddAIParams{
		DeckID: deckID,
		Lines:  lines,
		Mode:   app.BatchModeBot,
		DryRun: false,
	})
	if err != nil {
		return h.sendText(chatID, "Failed to add cards in batch: "+userFriendlyError(err))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Batch summary: total=%d created=%d skipped_duplicates=%d failed=%d",
		report.Summary.Total,
		report.Summary.Created,
		report.Summary.SkippedDuplicates,
		report.Summary.Failed,
	)
	for _, item := range report.Items {
		if item.Status == app.BatchAddStatusCreated {
			continue
		}
		reason := strings.TrimSpace(item.Reason)
		if reason == "" {
			reason = string(item.Status)
		}
		fmt.Fprintf(&b, "\n- %s => %s (%s)", item.FrontNormalized, item.Status, reason)
	}
	return h.sendText(chatID, b.String())
}

func (h *handler) handleNextCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	return h.sendNextCard(ctx, msg.Chat.ID, userID)
}

func (h *handler) sendNextCard(ctx context.Context, chatID int64, userID int64) error {
	card, stats, err := h.service.NextCardWithStatsForActiveDeckForUser(ctx, userID)
	if err != nil {
		if errors.Is(err, app.ErrActiveDeckNotSet) {
			return h.sendText(chatID, "Active deck is not set. Use /deck_use <name...>.")
		}
		return h.sendText(chatID, "Failed to fetch next card: "+userFriendlyError(err))
	}
	if card == nil {
		return h.sendText(chatID, "No available cards right now.")
	}

	reverse := false
	if h.randReverse != nil {
		reverse = h.randReverse()
	}
	msg := tgbotapi.NewMessage(chatID, renderCardMessage(*card, stats, reverse))
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = actionKeyboard(card.ID, card.DeckID)
	_, err = h.sendWithRetry(msg)
	return err
}

func (h *handler) sendText(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = mainReplyKeyboard()
	_, err := h.sendWithRetry(msg)
	return err
}

func (h *handler) sendReminderMessage(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := h.sendWithRetry(msg)
	return err
}

func runReminderLoop(ctx context.Context, h *handler, intervalMin int, minOverdue int, minHoursSinceReview float64) {
	if intervalMin <= 0 {
		return
	}
	ticker := time.NewTicker(time.Duration(intervalMin) * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runReminderTick(ctx, h, minOverdue, minHoursSinceReview, time.Now())
		}
	}
}

func runReminderTick(ctx context.Context, h *handler, minOverdue int, minHoursSinceReview float64, now time.Time) {
	for userID := range h.allow {
		eligible, overdueCount, err := h.service.ReminderEligible(ctx, userID, now, minOverdue, minHoursSinceReview)
		if err != nil {
			h.log.Warn("reminder check", "user_id", userID, "error", err)
			continue
		}
		if !eligible {
			continue
		}
		text := fmt.Sprintf("You have %d cards due for review. Tap Start learning to continue.", overdueCount)
		if err := h.sendReminderMessage(userID, text); err != nil {
			h.log.Warn("send reminder", "user_id", userID, "error", err)
		}
	}
}

func (h *handler) sendSwitchDeckMenu(ctx context.Context, chatID int64, userID int64) error {
	decks, err := h.service.ListDecksForUser(ctx, userID)
	if err != nil {
		return h.sendText(chatID, "Failed to list decks: "+userFriendlyError(err))
	}
	if len(decks) == 0 {
		return h.sendText(chatID, "No decks found.")
	}
	msg := tgbotapi.NewMessage(chatID, "Choose deck:")
	msg.ReplyMarkup = deckSwitchKeyboard(decks)
	_, err = h.sendWithRetry(msg)
	return err
}

func (h *handler) sendBatchAIDeckMenu(ctx context.Context, chatID int64, userID int64) error {
	decks, err := h.service.ListDecksForUser(ctx, userID)
	if err != nil {
		return h.sendText(chatID, "Failed to list decks: "+userFriendlyError(err))
	}
	if len(decks) == 0 {
		return h.sendText(chatID, "No decks found.")
	}
	msg := tgbotapi.NewMessage(chatID, "Choose deck for batch AI:")
	msg.ReplyMarkup = batchAIDeckKeyboard(decks)
	_, err = h.sendWithRetry(msg)
	return err
}

func (h *handler) setAwaitBatchAI(userID int64, state batchAwaitState) {
	h.batchAwaitMu.Lock()
	defer h.batchAwaitMu.Unlock()
	if h.batchAwaitNext == nil {
		h.batchAwaitNext = make(map[int64]batchAwaitState)
	}
	h.batchAwaitNext[userID] = state
}

func (h *handler) consumeAwaitBatchAI(userID int64) (batchAwaitState, bool) {
	h.batchAwaitMu.Lock()
	defer h.batchAwaitMu.Unlock()
	if h.batchAwaitNext == nil {
		return batchAwaitState{}, false
	}
	state, ok := h.batchAwaitNext[userID]
	if !ok {
		return batchAwaitState{}, false
	}
	delete(h.batchAwaitNext, userID)
	return state, true
}

func (h *handler) findDeckForUserByID(ctx context.Context, userID int64, deckID int64) (domain.Deck, bool, error) {
	decks, err := h.service.ListDecksForUser(ctx, userID)
	if err != nil {
		return domain.Deck{}, false, err
	}
	for _, d := range decks {
		if d.ID == deckID {
			return d, true, nil
		}
	}
	return domain.Deck{}, false, nil
}

func (h *handler) isAllowed(userID int64) bool {
	if len(h.allow) == 0 {
		return true
	}
	_, ok := h.allow[userID]
	return ok
}

func buildAllowlist(ids []int64) map[int64]struct{} {
	allow := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		allow[id] = struct{}{}
	}
	return allow
}

func (h *handler) sendWithRetry(msg tgbotapi.Chattable) (tgbotapi.Message, error) {
	var lastErr error
	delay := 200 * time.Millisecond
	for attempt := 0; attempt < 3; attempt++ {
		resp, err := h.api.Send(msg)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isRetryable(err) {
			break
		}
		time.Sleep(delay)
		delay *= 2
	}
	return tgbotapi.Message{}, lastErr
}

func (h *handler) answerCallback(callbackID string, text string) error {
	cfg := tgbotapi.NewCallback(callbackID, text)
	var lastErr error
	delay := 150 * time.Millisecond
	for attempt := 0; attempt < 3; attempt++ {
		_, err := h.api.Request(cfg)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryable(err) {
			break
		}
		time.Sleep(delay)
		delay *= 2
	}
	return lastErr
}

// notifyAndReturn reports msg to user via answerCallback and returns nil.
// Use when the error is user-facing and should not propagate.
func (h *handler) notifyAndReturn(callbackID, msg string, _ error) error {
	_ = h.answerCallback(callbackID, msg)
	return nil
}

// userFriendlyError returns a user-facing message for err, never technical details.
func userFriendlyError(err error) string {
	if msg := app.UserFriendlyMessage(err); msg != "" {
		return msg
	}
	return "Something went wrong. Please try again."
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "too many requests") || strings.Contains(msg, "temporarily unavailable")
}

func helpMessage() string {
	return strings.TrimSpace(`Available commands:

/start - show welcome message
/help - show this help
/whoami - show your Telegram user ID
/cancel - exit deck creation, import, or batch AI flow
/deck_create - create deck (guided flow if no args, or /deck_create <from> <to> <name>)
/deck_list - list your decks
/deck_use <name...> - set active deck by exact name
/deck_current - show active deck
/deck_export - export deck to JSON (choose deck, receive file named after deck)
/deck_import - upload .json file, then choose deck or create new one to add cards to
/card_add <front> | <back> | <pronunciation> | <example> | <conjugation> - add card to active deck
/card_add_batch_ai then newline-separated fronts - add cards via AI to active deck
/next - show next due card from active deck with action buttons`) // raw user-visible text
}

func parseDeckCreateArgs(args string) (string, string, string, error) {
	parts := strings.Fields(args)
	if len(parts) < 3 {
		return "", "", "", fmt.Errorf("usage: /deck_create <from> <to> <name...>")
	}
	languageFrom := parts[0]
	languageTo := parts[1]
	name := strings.Join(parts[2:], " ")
	return name, languageFrom, languageTo, nil
}

func parseCardAddArgs(args string) (string, string, string, string, string, error) {
	segments := strings.Split(args, "|")
	for i := range segments {
		segments[i] = strings.TrimSpace(segments[i])
	}
	if len(segments) < 2 {
		return "", "", "", "", "", fmt.Errorf("usage: /card_add <front> | <back> | <pronunciation> | <example> | <conjugation>")
	}
	front := segments[0]
	back := segments[1]
	pronunciation := ""
	example := ""
	conjugation := ""
	if len(segments) >= 3 {
		pronunciation = segments[2]
	}
	if len(segments) >= 4 {
		example = segments[3]
	}
	if len(segments) >= 5 {
		conjugation = segments[4]
	}
	return front, back, pronunciation, example, conjugation, nil
}

func parseCardAddBatchAIArgs(args string) ([]string, error) {
	normalized := strings.ReplaceAll(args, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return nil, fmt.Errorf("usage: /card_add_batch_ai followed by newline-separated fronts")
	}
	return strings.Split(normalized, "\n"), nil
}

func actionKeyboard(cardID, deckID int64) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("❌ Don't remember", fmt.Sprintf("act=dont_remember;card=%d;deck=%d", cardID, deckID)),
			tgbotapi.NewInlineKeyboardButtonData("✅ Remember", fmt.Sprintf("act=remember;card=%d;deck=%d", cardID, deckID)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🗑️ Remove", fmt.Sprintf("act=remove;card=%d;deck=%d", cardID, deckID)),
		),
	)
}

func deckSwitchKeyboard(decks []domain.Deck) tgbotapi.InlineKeyboardMarkup {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(decks)+1)
	for _, d := range decks {
		label := fmt.Sprintf("Use %s (%s->%s)", d.Name, d.LanguageFrom, d.LanguageTo)
		payload := fmt.Sprintf("act=use_deck;deck=%d", d.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, payload)))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("Create deck", "act=create_deck_start")))
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func createDeckOnlyKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("Create deck", "act=create_deck_start")),
	)
}

func mainReplyKeyboard() tgbotapi.ReplyKeyboardMarkup {
	return tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(startLearningButtonText),
			tgbotapi.NewKeyboardButton(addBatchAIButtonText),
		),
	)
}

func batchAIDeckKeyboard(decks []domain.Deck) tgbotapi.InlineKeyboardMarkup {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(decks))
	for _, d := range decks {
		label := fmt.Sprintf("Add to %s (%s->%s)", d.Name, d.LanguageFrom, d.LanguageTo)
		payload := fmt.Sprintf("act=batch_ai_deck;deck=%d", d.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, payload)))
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func exportDeckKeyboard(decks []domain.Deck) tgbotapi.InlineKeyboardMarkup {
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(decks))
	for _, d := range decks {
		label := fmt.Sprintf("Export %s (%s->%s)", d.Name, d.LanguageFrom, d.LanguageTo)
		payload := fmt.Sprintf("act=export_deck;deck=%d", d.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, payload)))
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func parseKVPayload(data, pairSeparator, kvSeparator string) map[string]string {
	parts := strings.Split(data, pairSeparator)
	fields := make(map[string]string, len(parts))
	for _, p := range parts {
		kv := strings.SplitN(p, kvSeparator, 2)
		if len(kv) != 2 {
			continue
		}
		fields[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return fields
}

func parsePositiveInt(raw, errorMessage string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s", errorMessage)
	}
	return value, nil
}

func renderCardMessage(card domain.Card, stats app.DeckStats, reverse bool) string {
	front := html.EscapeString(card.Front)
	back := html.EscapeString(card.Back)
	pron := html.EscapeString(card.Pronunciation)
	example := html.EscapeString(card.Example)
	conjugation := html.EscapeString(card.Conjugation)

	var visible string
	hiddenLines := make([]string, 0, 4)
	if reverse {
		visible = back
		if front != "" {
			hiddenLines = append(hiddenLines, front)
		}
	} else {
		visible = front
		if back != "" {
			hiddenLines = append(hiddenLines, back)
		}
	}
	if pron != "" {
		hiddenLines = append(hiddenLines, pron)
	}
	if conjugation != "" {
		hiddenLines = append(hiddenLines, conjugation)
	}
	if example != "" {
		hiddenLines = append(hiddenLines, example)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s</b>\n", visible)
	if len(hiddenLines) > 0 {
		fmt.Fprintf(&b, "<tg-spoiler>%s</tg-spoiler>\n", strings.Join(hiddenLines, "\n"))
	}
	fmt.Fprintf(&b, "\nActive %d, postponed %d, total %d", stats.Active, stats.Postponed, stats.Total)
	return b.String()
}
