package models

import (
	"github.com/google/uuid"
	"time"
)

type AIUsage struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID          uuid.UUID `gorm:"type:uuid;not null"`
	JobID           uuid.UUID `gorm:"type:uuid;not null"`
	Provider        string
	Model           string
	Operation       string
	ProviderTaskID  *string
	Status          string
	StartedAt       time.Time
	CompletedAt     *time.Time
	LatencyMS       *int64
	InputTokens     *int64
	OutputTokens    *int64
	TotalTokens     *int64
	GeneratedImages *int64
	DurationSeconds *int64
}

func (AIUsage) TableName() string { return "ai_usage" }
