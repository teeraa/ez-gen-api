package models

import (
	"time"

	"gorm.io/datatypes"
)

type ExportStatus string

const (
	ExportStatusQueued     ExportStatus = "queued"
	ExportStatusProcessing ExportStatus = "processing"
	ExportStatusSucceeded  ExportStatus = "succeeded"
	ExportStatusFailed     ExportStatus = "failed"
)

func (s ExportStatus) IsTerminal() bool {
	return s == ExportStatusSucceeded || s == ExportStatusFailed
}

// StoryboardExport represents a request to merge scene clips into a single video
// with optional background audio and text overlays.
type StoryboardExport struct {
	Base

	UserID string `gorm:"type:uuid;not null;index"`

	// Ordered scene IDs (JSON array of UUID strings)
	SceneIDs datatypes.JSON `gorm:"type:jsonb;not null"`

	// Optional background audio (references an Upload record's storage key)
	AudioStorageKey *string
	AudioVolume     float64 `gorm:"not null;default:1.0"`

	// Optional per-scene text overlays (JSON array of StoredTextLayer)
	TextLayers datatypes.JSON `gorm:"type:jsonb"`

	Status ExportStatus `gorm:"not null;default:'queued'"`

	OutputStorageKey *string
	OutputDurationMs *int64

	FailureMessage *string

	StartedAt    *time.Time
	CompletedAt  *time.Time
	LastPickedAt *time.Time
}

func (StoryboardExport) TableName() string { return "storyboard_exports" }

// StoredTextLayer is the JSON shape persisted in text_layers column.
type StoredTextLayer struct {
	SceneID  string `json:"sceneId"`
	Text     string `json:"text"`
	Position string `json:"position"` // "top" | "center" | "bottom"
	FontSize int    `json:"fontSize"`  // 0 = default
	Color    string `json:"color"`     // hex without #, "" = default
}
