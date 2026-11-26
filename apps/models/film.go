package models

import (
	"time"

	"github.com/google/uuid"
)

// FilmElement is a reusable character, location or prop of one film series (ภาพยนตร์).
type FilmElement struct {
	Base
	UserID         uuid.UUID `gorm:"type:uuid;not null"`
	SeriesID       uuid.UUID `gorm:"type:uuid;not null"`
	Kind           string
	Name           string
	Spec           string
	ImageKey       *string
	SyntheticKey   *string
	ImageStatus    string
	FailureCode    *string
	FailureMessage *string
	Revision       int
	// Voice is a CharacterVoice JSON (characters only); NULL means no voice.
	Voice []byte `gorm:"type:jsonb"`
}

func (FilmElement) TableName() string { return "film_elements" }

// FilmEpisode holds the film-menu document of one episode (a director_projects row of a film series).
type FilmEpisode struct {
	ProjectID uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid;not null"`
	Document  []byte    `gorm:"type:jsonb"`
	Revision  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (FilmEpisode) TableName() string { return "film_episodes" }

type FilmJob struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey"`
	UserID     uuid.UUID  `gorm:"type:uuid;not null"`
	SeriesID   uuid.UUID  `gorm:"type:uuid;not null"`
	ProjectID  *uuid.UUID `gorm:"type:uuid"`
	Kind       string
	TargetID   string
	Status     string
	Snapshot   []byte `gorm:"type:jsonb"`
	Output     []byte
	Cost       int
	Refunded   bool
	LeaseToken *uuid.UUID `gorm:"type:uuid"`
	LeaseUntil *time.Time
	// Attempts counts consecutive claims after a lease ran out without the worker finishing or
	// parking the job (a crash); LastError is the latest crash.
	Attempts  int
	LastError *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (FilmJob) TableName() string { return "film_jobs" }

type FilmReceipt struct {
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	Key       string    `gorm:"primaryKey"`
	Hash      string
	TargetID  string
	CreatedAt time.Time
}

func (FilmReceipt) TableName() string { return "film_receipts" }

type FilmPointEntry struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid;not null"`
	JobID     uuid.UUID `gorm:"type:uuid;not null"`
	Kind      string
	Amount    int
	CreatedAt time.Time
}

func (FilmPointEntry) TableName() string { return "film_point_entries" }
