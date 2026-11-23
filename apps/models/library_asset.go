package models

import "github.com/google/uuid"

type LibraryAsset struct {
	Base
	UserID      uuid.UUID `gorm:"type:uuid;not null;index"`
	Name        string
	AssetType   string
	Description string
	Prompt      string
	Scope       string
	Version     int
	JobID       *uuid.UUID `gorm:"type:uuid"`
	UploadID    *uuid.UUID `gorm:"type:uuid"`
	StorageKey  *string
	MediaType   *string
	// Voice is a CharacterVoice JSON (characters only); NULL means no voice.
	Voice []byte `gorm:"type:jsonb"`
}

func (LibraryAsset) TableName() string { return "library_assets" }
