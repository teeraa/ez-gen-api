package models

import "github.com/google/uuid"

type CharacterProfile struct {
	Base

	UserID uuid.UUID `gorm:"type:uuid;not null;index"`

	Name              string  `gorm:"not null"`
	Description       *string
	Appearance        *string
	Hairstyle         *string
	Clothing          *string
	Accessories       *string
	DefiningFeatures  *string
	ConsistencyNotes  *string

	// Incremented on each update so historical job snapshots can reference this version
	Version  int  `gorm:"not null;default:1"`
	IsActive bool `gorm:"not null;default:true"`

	Tags   []DNATag `gorm:"many2many:character_profile_tags;"`
	Assets []CharacterProfileAsset
}

func (CharacterProfile) TableName() string { return "character_profiles" }

type CharacterProfileTag struct {
	CharacterProfileID uuid.UUID `gorm:"type:uuid;primaryKey"`
	TagID              uuid.UUID `gorm:"type:uuid;primaryKey"`
}

func (CharacterProfileTag) TableName() string { return "character_profile_tags" }

type CharacterProfileAsset struct {
	Base
	CharacterProfileID uuid.UUID `gorm:"type:uuid;not null;index"`
	UploadID           uuid.UUID `gorm:"type:uuid;not null"`
	Role               string    `gorm:"not null;default:'reference'"` // 'reference' | 'primary'
	SortOrder          int       `gorm:"not null;default:0"`
	Upload             Upload
}

func (CharacterProfileAsset) TableName() string { return "character_profile_assets" }
