package bot

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"word-learning/internal/ai"
	"word-learning/internal/app"
	"word-learning/internal/domain"
	"word-learning/internal/storage/sqlite"
)

type telegramAPI interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
}

type handler struct {
	api            telegramAPI
	service        *app.Service
	log            *slog.Logger
	dedupe         *callbackDeduper
	allow          map[int64]struct{}
	newAIGenerator func() (ai.Generator, error)
	batchAwaitMu   sync.Mutex
	batchAwaitNext map[int64]batchAwaitState
}

type batchAwaitState struct {
	deckID int64
}

type commandHandler func(context.Context, *tgbotapi.Message, int64) error
type callbackActionHandler func(context.Context, int64, int64) error

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

	api, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		return fmt.Errorf("create telegram bot api: %w", err)
	}

	h := &handler{
		api:            api,
		service:        app.NewService(store),
		log:            logger,
		dedupe:         newCallbackDeduper(),
		allow:          buildAllowlist(cfg.AllowedUserIDs),
		newAIGenerator: ai.NewGeneratorFromEnv,
	}

	updateCfg := tgbotapi.NewUpdate(0)
	updateCfg.Timeout = cfg.PollingTimeout
	updates := api.GetUpdatesChan(updateCfg)
	logger.Info("telegram bot started", "bot_username", api.Self.UserName, "timeout_seconds", cfg.PollingTimeout)

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
		if update.Message.From == nil {
			return nil
		}
		if !h.isAllowed(update.Message.From.ID) {
			h.log.Warn("deny message from non-allowlisted user", "user_id", update.Message.From.ID)
			return h.sendText(update.Message.Chat.ID, "Access denied.")
		}
		if state, ok := h.consumeAwaitBatchAI(update.Message.From.ID); ok {
			return h.handleBatchAIInputMessage(ctx, update.Message, update.Message.From.ID, state)
		}
		if update.Message.IsCommand() {
			return h.handleCommand(ctx, update.Message)
		}
		return h.handleTextMessage(ctx, update.Message)
	}

	if update.CallbackQuery != nil {
		if update.CallbackQuery.From == nil {
			return nil
		}
		if !h.isAllowed(update.CallbackQuery.From.ID) {
			h.log.Warn("deny callback from non-allowlisted user", "user_id", update.CallbackQuery.From.ID)
			_ = h.answerCallback(update.CallbackQuery.ID, "Access denied.")
			return nil
		}
		return h.handleCallback(ctx, update.CallbackQuery)
	}

	return nil
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

	target, ok := h.resolveCallbackTarget(ctx, cb)
	if !ok {
		return nil
	}
	if err := h.executeCallbackAction(ctx, target); err != nil {
		_ = h.answerCallback(target.callbackID, "Action failed")
		return nil
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
		_ = h.answerCallback(cb.ID, "Invalid action payload")
		return nil
	}
	deck, err := h.service.DeckUseByIDForUser(ctx, cb.From.ID, deckID)
	if err != nil {
		_ = h.answerCallback(cb.ID, "Deck not found")
		return nil
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
		_ = h.answerCallback(cb.ID, "Invalid action payload")
		return nil
	}
	deck, found, err := h.findDeckForUserByID(ctx, cb.From.ID, deckID)
	if err != nil {
		return h.sendText(cb.Message.Chat.ID, fmt.Sprintf("Failed to list decks: %v", err))
	}
	if !found {
		_ = h.answerCallback(cb.ID, "Deck not found")
		return nil
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

func (h *handler) handleDeckCreateCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	name, langFrom, langTo, err := parseDeckCreateArgs(msg.CommandArguments())
	if err != nil {
		return h.sendText(msg.Chat.ID, err.Error())
	}
	deck, err := h.service.CreateDeckForUser(ctx, userID, name, langFrom, langTo)
	if err != nil {
		return h.sendText(msg.Chat.ID, fmt.Sprintf("Failed to create deck: %v", err))
	}
	return h.sendText(msg.Chat.ID, fmt.Sprintf("Deck created: id=%d name=%q pair=%s->%s", deck.ID, deck.Name, deck.LanguageFrom, deck.LanguageTo))
}

func (h *handler) handleDeckListCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	decks, err := h.service.ListDecksForUser(ctx, userID)
	if err != nil {
		return h.sendText(msg.Chat.ID, fmt.Sprintf("Failed to list decks: %v", err))
	}
	if len(decks) == 0 {
		return h.sendText(msg.Chat.ID, "No decks found.")
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
		return h.sendText(msg.Chat.ID, err.Error())
	}
	if result.Deck == nil {
		return h.sendText(msg.Chat.ID, "Failed to set active deck.")
	}
	return h.sendText(msg.Chat.ID, fmt.Sprintf("Active deck: %s (%s->%s)", result.Deck.Name, result.Deck.LanguageFrom, result.Deck.LanguageTo))
}

func (h *handler) handleDeckCurrentCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	deck, err := h.service.DeckCurrentForUser(ctx, userID)
	if err != nil {
		return h.sendText(msg.Chat.ID, fmt.Sprintf("Failed to resolve active deck: %v", err))
	}
	if deck == nil {
		return h.sendText(msg.Chat.ID, "Active deck is not set. Use /deck_use <name...>.")
	}
	return h.sendText(msg.Chat.ID, fmt.Sprintf("Active deck: %s (%s->%s)", deck.Name, deck.LanguageFrom, deck.LanguageTo))
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
		return h.sendText(msg.Chat.ID, fmt.Sprintf("Failed to add card: %v", err))
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
		return h.sendText(chatID, fmt.Sprintf("Failed to resolve active deck: %v", err))
	}
	return h.runBatchAIGenerationForDeck(ctx, chatID, userID, deck.ID, lines)
}

func (h *handler) runBatchAIGenerationForDeck(ctx context.Context, chatID int64, userID int64, deckID int64, lines []string) error {
	generator, err := h.newAIGenerator()
	if err != nil {
		return h.sendText(chatID, fmt.Sprintf("Failed to configure AI: %v", err))
	}
	report, err := h.service.AddCardsBatchAIForUser(ctx, userID, generator, app.BatchAddAIParams{
		DeckID: deckID,
		Lines:  lines,
		Mode:   app.BatchModeBot,
		DryRun: false,
	})
	if err != nil {
		return h.sendText(chatID, fmt.Sprintf("Failed to add cards in batch: %v", err))
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
		return h.sendText(chatID, fmt.Sprintf("Failed to fetch next card: %v", err))
	}
	if card == nil {
		return h.sendText(chatID, "No available cards right now.")
	}

	msg := tgbotapi.NewMessage(chatID, renderCardMessage(*card, stats))
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

func (h *handler) sendSwitchDeckMenu(ctx context.Context, chatID int64, userID int64) error {
	decks, err := h.service.ListDecksForUser(ctx, userID)
	if err != nil {
		return h.sendText(chatID, fmt.Sprintf("Failed to list decks: %v", err))
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
		return h.sendText(chatID, fmt.Sprintf("Failed to list decks: %v", err))
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
/deck_create <from> <to> <name...> - create deck
/deck_list - list your decks
/deck_use <name...> - set active deck by exact name
/deck_current - show active deck
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
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(decks))
	for _, d := range decks {
		label := fmt.Sprintf("Use %s (%s->%s)", d.Name, d.LanguageFrom, d.LanguageTo)
		payload := fmt.Sprintf("act=use_deck;deck=%d", d.ID)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(label, payload)))
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
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

func renderCardMessage(card domain.Card, stats app.DeckStats) string {
	front := html.EscapeString(card.Front)
	back := html.EscapeString(card.Back)
	pron := html.EscapeString(card.Pronunciation)
	example := html.EscapeString(card.Example)
	conjugation := html.EscapeString(card.Conjugation)

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s</b>\n", front)

	hiddenLines := make([]string, 0, 4)
	if back != "" {
		hiddenLines = append(hiddenLines, back)
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
	if len(hiddenLines) > 0 {
		fmt.Fprintf(&b, "<tg-spoiler>%s</tg-spoiler>\n", strings.Join(hiddenLines, "\n"))
	}
	fmt.Fprintf(&b, "\nActive %d, postponed %d, total %d", stats.Active, stats.Postponed, stats.Total)
	return b.String()
}
