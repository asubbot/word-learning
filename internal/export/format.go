package export

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"word-learning/internal/domain"
)

const ExportVersion = 1

// DeckMeta holds deck metadata for export/import.
type DeckMeta struct {
	Name         string `json:"name"`
	LanguageFrom string `json:"language_from"`
	LanguageTo   string `json:"language_to"`
}

// CardContent holds card content for export/import (no SRS state).
type CardContent struct {
	Front         string `json:"front"`
	Back          string `json:"back"`
	Pronunciation string `json:"pronunciation"`
	Example       string `json:"example"`
	Conjugation   string `json:"conjugation"`
}

// DeckExport is the root structure for deck export files.
type DeckExport struct {
	Version int           `json:"version"`
	Deck    DeckMeta      `json:"deck"`
	Cards   []CardContent `json:"cards"`
}

// MarshalExport builds JSON from a deck and its cards.
func MarshalExport(deck domain.Deck, cards []domain.Card) ([]byte, error) {
	cardContents := make([]CardContent, 0, len(cards))
	for _, c := range cards {
		cardContents = append(cardContents, CardContent{
			Front:         c.Front,
			Back:          c.Back,
			Pronunciation: c.Pronunciation,
			Example:       c.Example,
			Conjugation:   c.Conjugation,
		})
	}
	exp := DeckExport{
		Version: ExportVersion,
		Deck: DeckMeta{
			Name:         deck.Name,
			LanguageFrom: deck.LanguageFrom,
			LanguageTo:   deck.LanguageTo,
		},
		Cards: cardContents,
	}
	return json.MarshalIndent(exp, "", "  ")
}

// UnmarshalExport parses and validates export JSON.
func UnmarshalExport(data []byte) (*DeckExport, error) {
	var exp DeckExport
	if err := json.Unmarshal(data, &exp); err != nil {
		return nil, fmt.Errorf("parse export: %w", err)
	}
	if exp.Version != ExportVersion {
		return nil, fmt.Errorf("unsupported export version %d (expected %d)", exp.Version, ExportVersion)
	}
	if strings.TrimSpace(exp.Deck.Name) == "" {
		return nil, fmt.Errorf("deck name must not be empty")
	}
	if strings.TrimSpace(exp.Deck.LanguageFrom) == "" {
		return nil, fmt.Errorf("language_from must not be empty")
	}
	if strings.TrimSpace(exp.Deck.LanguageTo) == "" {
		return nil, fmt.Errorf("language_to must not be empty")
	}
	for i, c := range exp.Cards {
		if strings.TrimSpace(c.Front) == "" {
			return nil, fmt.Errorf("card %d: front must not be empty", i+1)
		}
		if strings.TrimSpace(c.Back) == "" {
			return nil, fmt.Errorf("card %d: back must not be empty", i+1)
		}
	}
	return &exp, nil
}

var unsafeFilenameChars = regexp.MustCompile(`[\/\\:*?"<>|]`)

// ExportFilename returns a safe filename for deck export (e.g. "English_Basics.json").
func ExportFilename(deckName string) string {
	s := strings.TrimSpace(deckName)
	s = strings.ReplaceAll(s, " ", "_")
	s = unsafeFilenameChars.ReplaceAllString(s, "_")
	if s == "" {
		s = "deck"
	}
	return s + ".json"
}
