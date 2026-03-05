package export

import (
	"strings"
	"testing"
	"time"

	"word-learning/internal/domain"
)

func TestMarshalUnmarshalExport(t *testing.T) {
	deck := domain.Deck{
		TelegramUserID: 42,
		ID:             1,
		Name:           "English Basics",
		LanguageFrom:   "EN",
		LanguageTo:     "RU",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	cards := []domain.Card{
		{Front: "banished", Back: "изгнанный", Pronunciation: "/banished/", Example: "He was banished.", Conjugation: ""},
		{Front: "come up with", Back: "придумать", Pronunciation: "", Example: "", Conjugation: ""},
	}

	data, err := MarshalExport(deck, cards)
	if err != nil {
		t.Fatalf("MarshalExport: %v", err)
	}

	exp, err := UnmarshalExport(data)
	if err != nil {
		t.Fatalf("UnmarshalExport: %v", err)
	}
	if exp.Version != ExportVersion {
		t.Errorf("version: got %d, want %d", exp.Version, ExportVersion)
	}
	if exp.Deck.Name != deck.Name {
		t.Errorf("deck name: got %q, want %q", exp.Deck.Name, deck.Name)
	}
	if exp.Deck.LanguageFrom != deck.LanguageFrom {
		t.Errorf("language_from: got %q, want %q", exp.Deck.LanguageFrom, deck.LanguageFrom)
	}
	if exp.Deck.LanguageTo != deck.LanguageTo {
		t.Errorf("language_to: got %q, want %q", exp.Deck.LanguageTo, deck.LanguageTo)
	}
	if len(exp.Cards) != 2 {
		t.Fatalf("cards: got %d, want 2", len(exp.Cards))
	}
	if exp.Cards[0].Front != "banished" || exp.Cards[0].Back != "изгнанный" {
		t.Errorf("card 0: got front=%q back=%q", exp.Cards[0].Front, exp.Cards[0].Back)
	}
	if exp.Cards[1].Front != "come up with" || exp.Cards[1].Back != "придумать" {
		t.Errorf("card 1: got front=%q back=%q", exp.Cards[1].Front, exp.Cards[1].Back)
	}
}

func TestMarshalUnmarshalExport_EmptyCards(t *testing.T) {
	deck := domain.Deck{Name: "Empty", LanguageFrom: "EN", LanguageTo: "RU"}
	cards := []domain.Card{}

	data, err := MarshalExport(deck, cards)
	if err != nil {
		t.Fatalf("MarshalExport: %v", err)
	}

	exp, err := UnmarshalExport(data)
	if err != nil {
		t.Fatalf("UnmarshalExport: %v", err)
	}
	if len(exp.Cards) != 0 {
		t.Errorf("cards: got %d, want 0", len(exp.Cards))
	}
}

func TestMarshalUnmarshalExport_AllFields(t *testing.T) {
	deck := domain.Deck{Name: "Full", LanguageFrom: "EN", LanguageTo: "RU"}
	cards := []domain.Card{
		{
			Front:         "word",
			Back:          "слово",
			Pronunciation: "/wɜːrd/",
			Example:       "Example sentence.",
			Conjugation:   "verb form",
		},
	}

	data, err := MarshalExport(deck, cards)
	if err != nil {
		t.Fatalf("MarshalExport: %v", err)
	}

	exp, err := UnmarshalExport(data)
	if err != nil {
		t.Fatalf("UnmarshalExport: %v", err)
	}
	c := exp.Cards[0]
	if c.Pronunciation != "/wɜːrd/" || c.Example != "Example sentence." || c.Conjugation != "verb form" {
		t.Errorf("card fields: pron=%q example=%q conj=%q", c.Pronunciation, c.Example, c.Conjugation)
	}
}

func TestUnmarshalExport_InvalidVersion(t *testing.T) {
	raw := `{"version": 99, "deck": {"name": "X", "language_from": "EN", "language_to": "RU"}, "cards": []}`
	_, err := UnmarshalExport([]byte(raw))
	if err == nil {
		t.Fatal("expected error for invalid version")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("error should mention version: %v", err)
	}
}

func TestUnmarshalExport_EmptyDeckName(t *testing.T) {
	raw := `{"version": 1, "deck": {"name": "", "language_from": "EN", "language_to": "RU"}, "cards": []}`
	_, err := UnmarshalExport([]byte(raw))
	if err == nil {
		t.Fatal("expected error for empty deck name")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error should mention name: %v", err)
	}
}

func TestUnmarshalExport_EmptyFront(t *testing.T) {
	raw := `{"version": 1, "deck": {"name": "X", "language_from": "EN", "language_to": "RU"}, "cards": [{"front": "", "back": "y"}]}`
	_, err := UnmarshalExport([]byte(raw))
	if err == nil {
		t.Fatal("expected error for empty front")
	}
}

func TestUnmarshalExport_EmptyBack(t *testing.T) {
	raw := `{"version": 1, "deck": {"name": "X", "language_from": "EN", "language_to": "RU"}, "cards": [{"front": "x", "back": ""}]}`
	_, err := UnmarshalExport([]byte(raw))
	if err == nil {
		t.Fatal("expected error for empty back")
	}
}

func TestUnmarshalExport_InvalidJSON(t *testing.T) {
	_, err := UnmarshalExport([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestExportFilename(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"English Basics", "English_Basics.json"},
		{"  Trim  ", "Trim.json"},
		{"Single", "Single.json"},
		{"a/b", "a_b.json"},
		{"a:b*c?d\"e<f>g|h", "a_b_c_d_e_f_g_h.json"},
		{"", "deck.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExportFilename(tt.name)
			if got != tt.want {
				t.Errorf("ExportFilename(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
