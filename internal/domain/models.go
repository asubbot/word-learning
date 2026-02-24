package domain

import "time"

type CardStatus string

const (
	CardStatusActive  CardStatus = "active"
	CardStatusRemoved CardStatus = "removed"
)

type Deck struct {
	TelegramUserID int64
	ID             int64
	Name           string
	LanguageFrom   string
	LanguageTo     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Card struct {
	ID             int64
	DeckID         int64
	EntryID        int64
	Front          string
	Back           string
	Pronunciation  string
	Example        string
	Conjugation    string
	Status         CardStatus
	NextDueAt      time.Time
	IntervalSec    int64
	Ease           float64
	Lapses         int64
	LastReviewedAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
