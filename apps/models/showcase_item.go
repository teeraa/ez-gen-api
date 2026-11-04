package models

import (
	"time"

	"github.com/google/uuid"
)

// ShowcaseItem is an admin-curated video shown on the public landing page.
type ShowcaseItem struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	JobID     uuid.UUID `gorm:"type:uuid;not null;uniqueIndex"`
	Title     string    `gorm:"not null"`
	Subtitle  *string
	Slot      string     `gorm:"not null;default:reel"`
	SortOrder int        `gorm:"not null;default:0"`
	IsActive  bool       `gorm:"not null;default:true"`
	CreatedBy *uuid.UUID `gorm:"type:uuid"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (ShowcaseItem) TableName() string { return "showcase_items" }
