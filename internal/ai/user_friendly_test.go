package ai

import (
	"errors"
	"testing"
)

func TestUserFriendlyMessage_PromptNotFound(t *testing.T) {
	t.Parallel()
	err := &ProviderError{
		Op:        "x",
		Retryable: false,
		Err:       errors.New("prompt file for pair de-fr not found"),
	}
	got := UserFriendlyMessage(err)
	want := "This language pair is not supported for AI generation."
	if got != want {
		t.Errorf("UserFriendlyMessage() = %q, want %q", got, want)
	}
}

func TestUserFriendlyMessage_EmptyPrompt(t *testing.T) {
	t.Parallel()
	err := &ProviderError{
		Op:        "x",
		Retryable: false,
		Err:       errors.New("prompt file is empty"),
	}
	got := UserFriendlyMessage(err)
	want := "AI prompt is empty. Contact support."
	if got != want {
		t.Errorf("UserFriendlyMessage() = %q, want %q", got, want)
	}
}

func TestUserFriendlyMessage_Retryable(t *testing.T) {
	t.Parallel()
	err := &ProviderError{
		Op:        "request",
		Retryable: true,
		Err:       errors.New("timeout"),
	}
	got := UserFriendlyMessage(err)
	want := "AI service temporarily unavailable. Try again later."
	if got != want {
		t.Errorf("UserFriendlyMessage() = %q, want %q", got, want)
	}
}

func TestUserFriendlyMessage_UnknownProviderError(t *testing.T) {
	t.Parallel()
	err := &ProviderError{
		Op:        "decode",
		Retryable: false,
		Err:       errors.New("invalid json"),
	}
	got := UserFriendlyMessage(err)
	want := "AI generation failed. Try again later."
	if got != want {
		t.Errorf("UserFriendlyMessage() = %q, want %q", got, want)
	}
}

func TestUserFriendlyMessage_NonProviderError(t *testing.T) {
	t.Parallel()
	err := errors.New("some error")
	got := UserFriendlyMessage(err)
	if got != "" {
		t.Errorf("UserFriendlyMessage() = %q, want empty string", got)
	}
}

func TestUserFriendlyMessage_Nil(t *testing.T) {
	t.Parallel()
	got := UserFriendlyMessage(nil)
	if got != "" {
		t.Errorf("UserFriendlyMessage(nil) = %q, want empty string", got)
	}
}
