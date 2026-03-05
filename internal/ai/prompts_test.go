package ai

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAvailableLanguagePairs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prompt_en-ru.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt_pt-ru.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt_en-en.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	pairs := ListAvailableLanguagePairs(dir)
	if len(pairs) != 3 {
		t.Fatalf("expected 3 pairs, got %d", len(pairs))
	}
	// Sorted: EN-EN, EN-RU, PT-RU
	if pairs[0].From != "EN" || pairs[0].To != "EN" {
		t.Errorf("pair 0: got %s->%s", pairs[0].From, pairs[0].To)
	}
	if pairs[1].From != "EN" || pairs[1].To != "RU" {
		t.Errorf("pair 1: got %s->%s", pairs[1].From, pairs[1].To)
	}
	if pairs[2].From != "PT" || pairs[2].To != "RU" {
		t.Errorf("pair 2: got %s->%s", pairs[2].From, pairs[2].To)
	}
}

func TestListAvailableLanguagePairs_EmptyDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pairs := ListAvailableLanguagePairs(dir)
	if len(pairs) != 0 {
		t.Fatalf("expected 0 pairs, got %d", len(pairs))
	}
}

func TestListAvailableLanguagePairs_InvalidDir(t *testing.T) {
	t.Parallel()
	pairs := ListAvailableLanguagePairs(filepath.Join(t.TempDir(), "nonexistent"))
	if len(pairs) != 0 {
		t.Fatalf("expected 0 pairs for missing dir, got %d", len(pairs))
	}
}

func TestListAvailableLanguagePairs_SkipsInvalidFilenames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prompt_en-ru.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt_ab.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "prompt_x-y-z.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	pairs := ListAvailableLanguagePairs(dir)
	if len(pairs) != 1 {
		t.Fatalf("expected 1 valid pair, got %d", len(pairs))
	}
	if pairs[0].From != "EN" || pairs[0].To != "RU" {
		t.Errorf("got %s->%s", pairs[0].From, pairs[0].To)
	}
}
