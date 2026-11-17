package models

import (
	"time"

	"github.com/google/uuid"
)

// AIPointCharge is the state of one AI use billed to the shared point wallet
// (director_point_accounts): pending (inside the charging transaction), charged or refunded.
type AIPointCharge struct {
	Feature   string    `gorm:"primaryKey"`
	Ref       string    `gorm:"primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid;not null"`
	Status    string
	Amount    int
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (AIPointCharge) TableName() string { return "ai_point_charges" }

// AIPointEntry is one movement (charge or refund) in the append-only ledger.
type AIPointEntry struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid;not null"`
	Feature   string
	Ref       string
	Kind      string
	Amount    int
	CreatedAt time.Time
}

func (AIPointEntry) TableName() string { return "ai_point_entries" }
