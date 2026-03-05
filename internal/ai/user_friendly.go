package ai

import (
	"errors"
	"strings"
)

// UserFriendlyMessage returns a user-facing message for known AI errors.
// Use when displaying errors to end users instead of technical details.
func UserFriendlyMessage(err error) string {
	if err == nil {
		return ""
	}
	var pe *ProviderError
	if !errors.As(err, &pe) {
		return ""
	}
	msg := strings.ToLower(pe.Err.Error())
	if strings.Contains(msg, "not found") || strings.Contains(msg, "not found/readable") {
		return "This language pair is not supported for AI generation."
	}
	if strings.Contains(msg, "empty") {
		return "AI prompt is empty. Contact support."
	}
	if pe.Retryable {
		return "AI service temporarily unavailable. Try again later."
	}
	return "AI generation failed. Try again later."
}
