package models

import "github.com/google/uuid"

type StyleProfile struct {
	Base

	UserID uuid.UUID `gorm:"type:uuid;not null;index"`

	Name           string  `gorm:"not null"`
	Description    *string
	VisualStyle    *string
	ColorPalette   *string
	Lighting       *string
	Mood           *string
	CameraLanguage *string
	QualityNotes   *string

	IsActive bool `gorm:"not null;default:true"`

	Tags   []DNATag `gorm:"many2many:style_profile_tags;"`
	Assets []StyleProfileAsset
}

func (StyleProfile) TableName() string { return "style_profiles" }

type StyleProfileTag struct {
	StyleProfileID uuid.UUID `gorm:"type:uuid;primaryKey"`
	TagID          uuid.UUID `gorm:"type:uuid;primaryKey"`
}

func (StyleProfileTag) TableName() string { return "style_profile_tags" }

type StyleProfileAsset struct {
	Base
	StyleProfileID uuid.UUID `gorm:"type:uuid;not null;index"`
	UploadID       uuid.UUID `gorm:"type:uuid;not null"`
	Role           string    `gorm:"not null;default:'reference'"`
	SortOrder      int       `gorm:"not null;default:0"`
	Upload         Upload
}

func (StyleProfileAsset) TableName() string { return "style_profile_assets" }
