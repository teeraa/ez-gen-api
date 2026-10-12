package models

import (
	"time"

	"github.com/google/uuid"
)

// VideoShare is a revocable public link to a finished job. It has no soft delete:
// revoked_at is the only way a link stops working.
type VideoShare struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	JobID     uuid.UUID `gorm:"type:uuid;not null"`
	UserID    uuid.UUID `gorm:"type:uuid;not null"`
	Token     string    `gorm:"not null;uniqueIndex"`
	ViewCount int64     `gorm:"not null;default:0"`
	CreatedAt time.Time
	RevokedAt *time.Time

	Job *VideoGenerationJob
}

func (VideoShare) TableName() string { return "video_shares" }
