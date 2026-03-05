package app

import (
	"errors"
	"strings"
	"word-learning/internal/ai"
)

// UserFriendlyMessage returns a user-facing message for known errors.
// Use when displaying errors to end users instead of technical details.
// Returns empty string for unknown errors; caller should use a generic fallback.
func UserFriendlyMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrActiveDeckNotSet) {
		return "Active deck is not set. Use /deck_use <name...>."
	}
	if errors.Is(err, ErrCardAlreadyExists) {
		return "Card already exists in this deck."
	}
	if errors.Is(err, ErrDeckNameAmbiguous) {
		return "Deck name is ambiguous."
	}
	if friendly := ai.UserFriendlyMessage(err); friendly != "" {
		return friendly
	}
	s := err.Error()
	if strings.Contains(s, "does not exist") || strings.Contains(s, "not found") {
		return "Not found."
	}
	if strings.Contains(s, "must not be empty") || strings.Contains(s, "must be") {
		return "Invalid input. Check the command format."
	}
	if strings.Contains(s, "OPENAI_API_KEY") || strings.Contains(s, "OPENAI_PROMPTS_DIR") {
		return "AI is not configured. Check your settings."
	}
	return ""
}
