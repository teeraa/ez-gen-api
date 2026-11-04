package models

import (
	"github.com/google/uuid"
	"time"
)

type DirectorProject struct {
	Base
	UserID      uuid.UUID `gorm:"type:uuid;not null"`
	Name        string
	Document    []byte `gorm:"type:jsonb"`
	WorkVersion int64
}

func (DirectorProject) TableName() string { return "director_projects" }

type DirectorJob struct {
	Base
	UserID     uuid.UUID `gorm:"type:uuid;not null"`
	ProjectID  uuid.UUID `gorm:"type:uuid;not null"`
	Kind       string
	TargetID   string
	Status     string
	Snapshot   []byte `gorm:"type:jsonb"`
	Cost       int
	Refunded   bool
	Output     []byte
	LeaseToken *uuid.UUID `gorm:"type:uuid"`
	LeaseUntil *time.Time
}

func (DirectorJob) TableName() string { return "director_jobs" }

type DirectorReceipt struct {
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	Key       string    `gorm:"primaryKey"`
	Hash      string
	ProjectID uuid.UUID `gorm:"type:uuid;not null"`
	Response  []byte    `gorm:"type:jsonb"`
	CreatedAt time.Time
}

func (DirectorReceipt) TableName() string { return "director_receipts" }

type DirectorAccount struct {
	UserID  uuid.UUID `gorm:"type:uuid;primaryKey"`
	Balance int64
}

func (DirectorAccount) TableName() string { return "director_point_accounts" }

type DirectorPointEntry struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserID    uuid.UUID `gorm:"type:uuid;not null"`
	JobID     uuid.UUID `gorm:"type:uuid;not null"`
	Kind      string
	Amount    int
	CreatedAt time.Time
}

func (DirectorPointEntry) TableName() string { return "director_point_entries" }
