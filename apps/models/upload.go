package models

import (
	"time"

	"github.com/google/uuid"
)

type Upload struct {
	Base
	UserID     uuid.UUID `gorm:"type:uuid;not null;index"`
	StorageKey string    `gorm:"not null"`
	Filename   string    `gorm:"not null"`
	MimeType   string    `gorm:"not null"`
	SizeBytes  int64     `gorm:"not null"`
	ExpiresAt  time.Time `gorm:"not null"`
}

func (Upload) TableName() string { return "uploads" }
