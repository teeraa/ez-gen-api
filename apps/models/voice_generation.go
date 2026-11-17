package models

import (
	"time"

	"github.com/google/uuid"
)

// VoiceGeneration is one text-to-speech result of the Voice menu.
type VoiceGeneration struct {
	Base
	UserID           uuid.UUID  `gorm:"type:uuid;not null;index"`
	RequestID        uuid.UUID  `gorm:"column:request_id;type:uuid;not null"`
	Model            string     `gorm:"not null"`
	Text             string     `gorm:"not null"`
	VoiceID          string     `gorm:"column:voice_id;not null"`
	VoiceName        string     `gorm:"column:voice_name;not null;default:''"`
	ProfileID        *uuid.UUID `gorm:"column:profile_id;type:uuid"`
	Emotion          string     `gorm:"not null;default:''"`
	Speed            float64    `gorm:"not null;default:1"`
	Pitch            int        `gorm:"not null;default:0"`
	AudioKey         string     `gorm:"column:audio_key;not null"`
	SizeBytes        int64      `gorm:"column:size_bytes;not null;default:0"`
	Duration         float64    `gorm:"not null;default:0"`
	OriginalDuration float64    `gorm:"column:original_duration;not null;default:0"`
	Sentences        []byte     `gorm:"type:jsonb;not null;default:'[]'"`
}

func (VoiceGeneration) TableName() string { return "voice_generations" }

// VoiceProfile is a user's own voice in the Voice menu: a reference clip or a written voice description.
type VoiceProfile struct {
	Base
	UserID       uuid.UUID  `gorm:"type:uuid;not null;index"`
	Name         string     `gorm:"not null"`
	Kind         string     `gorm:"not null"`
	Description  string     `gorm:"not null;default:''"`
	AudioKey     *string    `gorm:"column:audio_key"`
	AudioSeconds float64    `gorm:"column:audio_seconds;not null;default:0"`
	AudioBytes   int64      `gorm:"column:audio_bytes;not null;default:0"`
	ConsentAt    *time.Time `gorm:"column:consent_at"`
}

func (VoiceProfile) TableName() string { return "voice_profiles" }
