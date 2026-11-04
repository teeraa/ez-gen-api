package models

import (
	"github.com/google/uuid"
	"time"
)

type StudioPipeline struct {
	Base
	UserID           uuid.UUID `gorm:"type:uuid;not null"`
	IdempotencyKey   string
	RequestHash      string
	Inputs           []byte `gorm:"type:jsonb"`
	Status           string
	Stage            string
	CharacterJobID   *uuid.UUID `gorm:"type:uuid"`
	CharacterAssetID *uuid.UUID `gorm:"type:uuid"`
	VideoJobID       *uuid.UUID `gorm:"type:uuid"`
	VideoAssetID     *uuid.UUID `gorm:"type:uuid"`
	VideoAttempt     int
	FailureCode      *string
	FailureMessage   *string
	LeaseToken       *uuid.UUID `gorm:"type:uuid"`
	LeaseUntil       *time.Time
}

func (StudioPipeline) TableName() string { return "studio_pipelines" }
