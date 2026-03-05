package app

import (
	"errors"
	"testing"
	"word-learning/internal/ai"
)

func TestUserFriendlyMessage_ErrActiveDeckNotSet(t *testing.T) {
	t.Parallel()
	got := UserFriendlyMessage(ErrActiveDeckNotSet)
	want := "Active deck is not set. Use /deck_use <name...>."
	if got != want {
		t.Errorf("UserFriendlyMessage() = %q, want %q", got, want)
	}
}

func TestUserFriendlyMessage_ErrCardAlreadyExists(t *testing.T) {
	t.Parallel()
	got := UserFriendlyMessage(ErrCardAlreadyExists)
	want := "Card already exists in this deck."
	if got != want {
		t.Errorf("UserFriendlyMessage() = %q, want %q", got, want)
	}
}

func TestUserFriendlyMessage_ErrDeckNameAmbiguous(t *testing.T) {
	t.Parallel()
	got := UserFriendlyMessage(ErrDeckNameAmbiguous)
	want := "Deck name is ambiguous."
	if got != want {
		t.Errorf("UserFriendlyMessage() = %q, want %q", got, want)
	}
}

func TestUserFriendlyMessage_NotFoundPattern(t *testing.T) {
	t.Parallel()
	err := errors.New("deck 1 does not exist")
	got := UserFriendlyMessage(err)
	want := "Not found."
	if got != want {
		t.Errorf("UserFriendlyMessage() = %q, want %q", got, want)
	}
}

func TestUserFriendlyMessage_OpenAIConfig(t *testing.T) {
	t.Parallel()
	err := errors.New("OPENAI_API_KEY is required")
	got := UserFriendlyMessage(err)
	want := "AI is not configured. Check your settings."
	if got != want {
		t.Errorf("UserFriendlyMessage() = %q, want %q", got, want)
	}
}

func TestUserFriendlyMessage_DelegatesToAI(t *testing.T) {
	t.Parallel()
	err := &ai.ProviderError{
		Op:        "resolve system prompt",
		Retryable: false,
		Err:       errors.New("prompt file for pair de-fr not found/readable"),
	}
	got := UserFriendlyMessage(err)
	want := "This language pair is not supported for AI generation."
	if got != want {
		t.Errorf("UserFriendlyMessage() = %q, want %q", got, want)
	}
}

func TestUserFriendlyMessage_Unknown(t *testing.T) {
	t.Parallel()
	err := errors.New("unknown")
	got := UserFriendlyMessage(err)
	if got != "" {
		t.Errorf("UserFriendlyMessage() = %q, want empty string", got)
	}
}
