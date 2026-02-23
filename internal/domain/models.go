package domain

import "time"

type CardStatus string

const (
	CardStatusActive  CardStatus = "active"
	CardStatusSnoozed CardStatus = "snoozed"
	CardStatusRemoved CardStatus = "removed"
)

type Deck struct {
	ID           int64
	Name         string
	LanguageFrom string
	LanguageTo   string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Card struct {
	ID           int64
	DeckID       int64
	Front        string
	Back         string
	Description  string
	Status       CardStatus
	SnoozedUntil *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
