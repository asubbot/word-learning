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
	"word-learning-cli/internal/ai"
	"word-learning-cli/internal/app"
	"word-learning-cli/internal/domain"
	"word-learning-cli/internal/storage/sqlite"
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
		if update.Message.IsCommand() {
			return h.handleCommand(ctx, update.Message)
		}
		return h.sendText(update.Message.Chat.ID, "Use /help to see available commands.")
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
	return h.sendNextCard(ctx, target.chatID, target.userID, target.deckID)
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
	return h.sendText(msg.Chat.ID, strings.TrimSpace(b.String()))
}

func (h *handler) handleCardAddCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	deckID, front, back, pronunciation, description, err := parseCardAddArgs(msg.CommandArguments())
	if err != nil {
		return h.sendText(msg.Chat.ID, err.Error())
	}
	card, err := h.service.AddCardForUser(ctx, userID, deckID, front, back, pronunciation, description)
	if err != nil {
		return h.sendText(msg.Chat.ID, fmt.Sprintf("Failed to add card: %v", err))
	}
	return h.sendText(msg.Chat.ID, fmt.Sprintf("Card created: id=%d deck=%d", card.ID, card.DeckID))
}

func (h *handler) handleCardAddBatchAICommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	deckID, lines, err := parseCardAddBatchAIArgs(msg.CommandArguments())
	if err != nil {
		return h.sendText(msg.Chat.ID, err.Error())
	}
	generator, err := h.newAIGenerator()
	if err != nil {
		return h.sendText(msg.Chat.ID, fmt.Sprintf("Failed to configure AI: %v", err))
	}
	report, err := h.service.AddCardsBatchAIForUser(ctx, userID, generator, app.BatchAddAIParams{
		DeckID: deckID,
		Lines:  lines,
		Mode:   app.BatchModeBot,
		DryRun: false,
	})
	if err != nil {
		return h.sendText(msg.Chat.ID, fmt.Sprintf("Failed to add cards in batch: %v", err))
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
	return h.sendText(msg.Chat.ID, b.String())
}

func (h *handler) handleNextCommand(ctx context.Context, msg *tgbotapi.Message, userID int64) error {
	deckID, err := parseDeckIDArg(msg.CommandArguments())
	if err != nil {
		return h.sendText(msg.Chat.ID, err.Error())
	}
	return h.sendNextCard(ctx, msg.Chat.ID, userID, deckID)
}

func (h *handler) sendNextCard(ctx context.Context, chatID int64, userID int64, deckID int64) error {
	card, stats, err := h.service.NextCardWithStatsForUser(ctx, userID, deckID)
	if err != nil {
		return h.sendText(chatID, fmt.Sprintf("Failed to fetch next card: %v", err))
	}
	if card == nil {
		return h.sendText(chatID, "No available cards right now.")
	}

	msg := tgbotapi.NewMessage(chatID, renderCardMessage(*card, stats))
	msg.ParseMode = "HTML"
	msg.ReplyMarkup = actionKeyboard(card.ID, deckID)
	_, err = h.sendWithRetry(msg)
	return err
}

func (h *handler) sendText(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := h.sendWithRetry(msg)
	return err
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
/card_add <deck_id> | <front> | <back> | <pronunciation> | <description> - add card
/card_add_batch_ai <deck_id> then newline-separated fronts - add cards via AI
/next <deck_id> - show next due card with action buttons`) // raw user-visible text
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

func parseDeckIDArg(args string) (int64, error) {
	value := strings.TrimSpace(args)
	if value == "" {
		return 0, fmt.Errorf("usage: /next <deck_id>")
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("deck_id must be a positive integer")
	}
	return id, nil
}

func parseCardAddArgs(args string) (int64, string, string, string, string, error) {
	segments := strings.Split(args, "|")
	for i := range segments {
		segments[i] = strings.TrimSpace(segments[i])
	}
	if len(segments) < 3 {
		return 0, "", "", "", "", fmt.Errorf("usage: /card_add <deck_id> | <front> | <back> | <pronunciation> | <description>")
	}
	deckID, err := strconv.ParseInt(segments[0], 10, 64)
	if err != nil || deckID <= 0 {
		return 0, "", "", "", "", fmt.Errorf("deck_id must be a positive integer")
	}
	front := segments[1]
	back := segments[2]
	pronunciation := ""
	description := ""
	if len(segments) >= 4 {
		pronunciation = segments[3]
	}
	if len(segments) >= 5 {
		description = segments[4]
	}
	return deckID, front, back, pronunciation, description, nil
}

func parseCardAddBatchAIArgs(args string) (int64, []string, error) {
	normalized := strings.ReplaceAll(args, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return 0, nil, fmt.Errorf("usage: /card_add_batch_ai <deck_id> followed by newline-separated fronts")
	}
	lines := strings.Split(normalized, "\n")
	if len(lines) < 2 {
		return 0, nil, fmt.Errorf("usage: /card_add_batch_ai <deck_id> followed by newline-separated fronts")
	}
	deckID, err := parsePositiveInt(strings.TrimSpace(lines[0]), "deck_id must be a positive integer")
	if err != nil {
		return 0, nil, err
	}
	return deckID, lines[1:], nil
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
	desc := html.EscapeString(card.Description)

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s</b>\n", front)

	hiddenLines := make([]string, 0, 3)
	if back != "" {
		hiddenLines = append(hiddenLines, back)
	}
	if pron != "" {
		hiddenLines = append(hiddenLines, pron)
	}
	if desc != "" {
		hiddenLines = append(hiddenLines, desc)
	}
	if len(hiddenLines) > 0 {
		fmt.Fprintf(&b, "<tg-spoiler>%s</tg-spoiler>\n", strings.Join(hiddenLines, "\n"))
	}
	fmt.Fprintf(&b, "\nActive %d, postponed %d, total %d", stats.Active, stats.Postponed, stats.Total)
	return b.String()
}
