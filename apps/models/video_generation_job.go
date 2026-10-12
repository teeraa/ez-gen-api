package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type JobStatus string

const (
	JobStatusQueued     JobStatus = "queued"
	JobStatusSubmitted  JobStatus = "submitted"
	JobStatusProcessing JobStatus = "processing"
	JobStatusSucceeded  JobStatus = "succeeded"
	JobStatusFailed     JobStatus = "failed"
	JobStatusCancelled  JobStatus = "cancelled"
)

func (s JobStatus) IsTerminal() bool {
	return s == JobStatusSucceeded || s == JobStatusFailed || s == JobStatusCancelled
}

type VideoGenerationJob struct {
	Base

	UserID uuid.UUID `gorm:"type:uuid;not null;index"`

	// Status lifecycle
	Status         JobStatus `gorm:"not null;default:'queued'"`
	ProviderTaskID *string   `gorm:"index"`
	ProviderStatus *string

	// Generation parameters (immutable after creation)
	Prompt          string    `gorm:"not null"`
	Mode            string    `gorm:"not null"`
	ModelID         string    `gorm:"not null"`
	AspectRatio     string    `gorm:"not null"`
	DurationSeconds int       `gorm:"not null"`
	Quality         string    `gorm:"not null;default:'standard'"`
	EnhancePrompt   bool      `gorm:"not null;default:false"`
	UploadID        *uuid.UUID `gorm:"type:uuid"`

	// VIOLET DNA links (set at job creation; immutable)
	SceneID            *uuid.UUID `gorm:"type:uuid;index"`
	CharacterProfileID *uuid.UUID `gorm:"type:uuid"`
	StyleProfileID     *uuid.UUID `gorm:"type:uuid"`

	// Snapshot of compiled inputs at submission (immutable; prevents profile edits from changing history)
	EffectiveInputs datatypes.JSON `gorm:"type:jsonb"`
	CompiledPrompt  *string

	// Output (set when succeeded)
	OutputMediaKey  *string
	OutputThumbKey  *string
	OutputMediaType *string
	OutputDurationMs *int

	// Progress
	Progress    int    `gorm:"not null;default:0"`
	CurrentStep *string

	// Failure
	FailureCode    *string
	FailureMessage *string
	ProviderError  datatypes.JSON `gorm:"type:jsonb"`

	// Credits
	EstimatedCredits *int
	ChargedCredits   *int

	// Submission tracking
	SubmittedAt  *time.Time
	CompletedAt  *time.Time
	LastPolledAt *time.Time

	// Idempotency
	IdempotencyKey *string `gorm:"uniqueIndex"`
}

func (VideoGenerationJob) TableName() string { return "video_generation_jobs" }
