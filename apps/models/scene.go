package models

import (
	"time"

	"github.com/google/uuid"
)

const MaxSceneCharacters = 3

type Scene struct {
	Base

	UserID uuid.UUID `gorm:"type:uuid;not null;index"`

	Title       *string
	Description *string

	CharacterProfileID *uuid.UUID `gorm:"type:uuid;index"`
	StyleProfileID     *uuid.UUID `gorm:"type:uuid;index"`

	// Scene chain — allows "Continue Scene" to link scenes sequentially
	PreviousSceneID *uuid.UUID `gorm:"type:uuid;index"`

	SceneOrder int `gorm:"not null;default:0"`

	// Per-scene overrides (applied on top of profile values in prompt compilation)
	ActionOverride  *string
	CameraOverride  *string
	SettingOverride *string

	// The generation job currently selected as the output for this scene
	SelectedJobID *uuid.UUID `gorm:"type:uuid"`

	Tags       []DNATag `gorm:"many2many:scene_tags;"`
	Assets     []SceneAsset
	Characters []SceneCharacter

	CharacterProfile *CharacterProfile
	StyleProfile     *StyleProfile
	PreviousScene    *Scene
}

func (Scene) TableName() string { return "scenes" }

type SceneTag struct {
	SceneID uuid.UUID `gorm:"type:uuid;primaryKey"`
	TagID   uuid.UUID `gorm:"type:uuid;primaryKey"`
}

func (SceneTag) TableName() string { return "scene_tags" }

// SceneCharacter orders the characters featured in a scene. CharacterProfileID on
// Scene mirrors the first entry for clients that predate multi-character scenes.
type SceneCharacter struct {
	SceneID            uuid.UUID `gorm:"type:uuid;primaryKey"`
	CharacterProfileID uuid.UUID `gorm:"type:uuid;primaryKey"`
	SortOrder          int       `gorm:"not null;default:0"`
	CreatedAt          time.Time

	CharacterProfile *CharacterProfile
}

func (SceneCharacter) TableName() string { return "scene_characters" }

type SceneAsset struct {
	Base
	SceneID   uuid.UUID `gorm:"type:uuid;not null;index"`
	UploadID  uuid.UUID `gorm:"type:uuid;not null"`
	Role      string    `gorm:"not null;default:'reference'"` // 'reference' | 'first_frame' | 'last_frame' | 'keyframe'
	SortOrder int       `gorm:"not null;default:0"`
	Upload    Upload
}

func (SceneAsset) TableName() string { return "scene_assets" }
