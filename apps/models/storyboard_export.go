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

	// Ordered scene IDs (JSON array of UUID strings). Kept populated for download/listing.
	SceneIDs datatypes.JSON `gorm:"type:jsonb;not null"`

	// Clips supersedes SceneIDs: ordered clips with optional per-clip trim (JSON array of StoredClip).
	Clips datatypes.JSON `gorm:"type:jsonb"`

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

// StoredClip is the JSON shape persisted in the clips column.
type StoredClip struct {
	SceneID   string  `json:"sceneId"`
	TrimStart float64 `json:"trimStart"` // seconds from clip start; 0 = from start
	TrimEnd   float64 `json:"trimEnd"`   // seconds from clip start; 0 = full duration
}

// StoredTextLayer is the JSON shape persisted in the text_layers column.
type StoredTextLayer struct {
	SceneID  string `json:"sceneId"`
	Text     string `json:"text"`
	Position string `json:"position"` // "top" | "center" | "bottom" (fallback when no X/Y)
	FontSize int    `json:"fontSize"` // px at 1080-height reference; 0 = default
	Color    string `json:"color"`    // hex without #, "" = default

	// Free placement (timeline editor). HasXY distinguishes an explicit (0,0) from unset.
	HasXY bool    `json:"hasXy"`
	X     float64 `json:"x"`     // normalized 0..1, centre anchor
	Y     float64 `json:"y"`     // normalized 0..1, centre anchor
	Align string  `json:"align"` // "left" | "center" | "right"

	// Timing relative to the clip's trimmed start; both 0 = whole clip.
	StartTime float64 `json:"startTime"`
	EndTime   float64 `json:"endTime"`
}
