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
	"word-learning-cli/internal/app"
	"word-learning-cli/internal/domain"
	"word-learning-cli/internal/storage/sqlite"
)

type telegramAPI interface {
	Send(c tgbotapi.Chattable) (tgbotapi.Message, error)
	Request(c tgbotapi.Chattable) (*tgbotapi.APIResponse, error)
}

type handler struct {
	api     telegramAPI
	service *app.Service
	log     *slog.Logger
	dedupe  *callbackDeduper
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
		api:     api,
		service: app.NewService(store),
		log:     logger,
		dedupe:  newCallbackDeduper(),
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
		if update.Message.IsCommand() {
			return h.handleCommand(ctx, update.Message)
		}
		return h.sendText(update.Message.Chat.ID, "Use /help to see available commands.")
	}

	if update.CallbackQuery != nil {
		return h.handleCallback(ctx, update.CallbackQuery)
	}

	return nil
}

func (h *handler) handleCommand(ctx context.Context, msg *tgbotapi.Message) error {
	command := strings.ToLower(msg.Command())
	userID := msg.From.ID

	switch command {
	case "start":
		return h.sendText(msg.Chat.ID, helpMessage())
	case "help":
		return h.sendText(msg.Chat.ID, helpMessage())
	case "health":
		return h.sendText(msg.Chat.ID, "OK")
	case "deck_create":
		name, langFrom, langTo, err := parseDeckCreateArgs(msg.CommandArguments())
		if err != nil {
			return h.sendText(msg.Chat.ID, err.Error())
		}
		deck, err := h.service.CreateDeckForUser(ctx, userID, name, langFrom, langTo)
		if err != nil {
			return h.sendText(msg.Chat.ID, fmt.Sprintf("Failed to create deck: %v", err))
		}
		return h.sendText(msg.Chat.ID, fmt.Sprintf("Deck created: id=%d name=%q pair=%s->%s", deck.ID, deck.Name, deck.LanguageFrom, deck.LanguageTo))
	case "deck_list":
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
	case "card_add":
		deckID, front, back, pronunciation, description, err := parseCardAddArgs(msg.CommandArguments())
		if err != nil {
			return h.sendText(msg.Chat.ID, err.Error())
		}
		card, err := h.service.AddCardForUser(ctx, userID, deckID, front, back, pronunciation, description)
		if err != nil {
			return h.sendText(msg.Chat.ID, fmt.Sprintf("Failed to add card: %v", err))
		}
		return h.sendText(msg.Chat.ID, fmt.Sprintf("Card created: id=%d deck=%d", card.ID, card.DeckID))
	case "next":
		deckID, err := parseDeckIDArg(msg.CommandArguments())
		if err != nil {
			return h.sendText(msg.Chat.ID, err.Error())
		}
		return h.sendNextCard(ctx, msg.Chat.ID, userID, deckID)
	default:
		return h.sendText(msg.Chat.ID, "Unknown command. Use /help.")
	}
}

func (h *handler) handleCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) error {
	if cb == nil || cb.Message == nil || cb.From == nil {
		return nil
	}
	if h.dedupe.Seen(cb.ID) {
		return h.answerCallback(cb.ID, "Already processed.")
	}

	action, cardID, deckID, err := parseCallbackData(cb.Data)
	if err != nil {
		_ = h.answerCallback(cb.ID, "Invalid action payload")
		return nil
	}

	card, err := h.service.GetCardByIDForUser(ctx, cb.From.ID, cardID)
	if err != nil {
		_ = h.answerCallback(cb.ID, "Card not found")
		return nil
	}
	if card.DeckID != deckID {
		_ = h.answerCallback(cb.ID, "Card/deck mismatch")
		return nil
	}

	switch action {
	case "remember":
		err = h.service.RememberCardForUser(ctx, cb.From.ID, cardID)
	case "dont_remember":
		err = h.service.DontRememberCardForUser(ctx, cb.From.ID, cardID)
	case "remove":
		err = h.service.RemoveCardForUser(ctx, cb.From.ID, cardID)
	default:
		err = fmt.Errorf("unknown action")
	}
	if err != nil {
		_ = h.answerCallback(cb.ID, "Action failed")
		return nil
	}

	if err := h.answerCallback(cb.ID, "Done"); err != nil {
		h.log.Warn("answer callback", "error", err)
	}
	return h.sendNextCard(ctx, cb.Message.Chat.ID, cb.From.ID, deckID)
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
/health - health check
/deck_create <name> <from> <to> - create deck
/deck_list - list your decks
/card_add <deck_id> | <front> | <back> | <pronunciation> | <description> - add card
/next <deck_id> - show next due card with action buttons`) // raw user-visible text
}

func parseDeckCreateArgs(args string) (string, string, string, error) {
	parts := strings.Fields(args)
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("usage: /deck_create <name> <from> <to>")
	}
	return parts[0], parts[1], parts[2], nil
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

func parseCallbackData(data string) (string, int64, int64, error) {
	parts := strings.Split(data, ";")
	fields := make(map[string]string, len(parts))
	for _, p := range parts {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		fields[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	action := fields["act"]
	cardRaw := fields["card"]
	deckRaw := fields["deck"]
	if action == "" || cardRaw == "" || deckRaw == "" {
		return "", 0, 0, fmt.Errorf("missing callback fields")
	}
	cardID, err := strconv.ParseInt(cardRaw, 10, 64)
	if err != nil || cardID <= 0 {
		return "", 0, 0, fmt.Errorf("invalid card id")
	}
	deckID, err := strconv.ParseInt(deckRaw, 10, 64)
	if err != nil || deckID <= 0 {
		return "", 0, 0, fmt.Errorf("invalid deck id")
	}
	return action, cardID, deckID, nil
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
