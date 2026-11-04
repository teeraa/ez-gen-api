package models

import (
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type ProductionProject struct {
	Base
	UserID      uuid.UUID `gorm:"type:uuid;not null;index"`
	Title       string
	Brief       string
	AspectRatio string
	Revision    int
	EditDraft   datatypes.JSON `gorm:"type:jsonb"`
}

func (ProductionProject) TableName() string { return "production_projects" }

type VideoSettings struct {
	Model         string `json:"model"`
	Mode          string `json:"mode"`
	AspectRatio   string `json:"aspectRatio"`
	Duration      int    `json:"duration"`
	Quality       string `json:"quality"`
	EnhancePrompt bool   `json:"enhancePrompt"`
}
type EditClip struct {
	SceneID   string  `json:"sceneId"`
	JobID     *string `json:"jobId,omitempty"`
	TrimStart float64 `json:"trimStart"`
	TrimEnd   float64 `json:"trimEnd"`
	Enabled   bool    `json:"enabled"`
}
type EditTextLayer struct {
	SceneID   string   `json:"sceneId"`
	Text      string   `json:"text"`
	Position  string   `json:"position,omitempty"`
	FontSize  *int     `json:"fontSize,omitempty"`
	Color     *string  `json:"color,omitempty"`
	X         *float64 `json:"x,omitempty"`
	Y         *float64 `json:"y,omitempty"`
	Align     string   `json:"align,omitempty"`
	StartTime *float64 `json:"startTime,omitempty"`
	EndTime   *float64 `json:"endTime,omitempty"`
}
type EditDraft struct {
	Clips         []EditClip      `json:"clips"`
	AudioUploadID *string         `json:"audioUploadId,omitempty"`
	AudioVolume   float64         `json:"audioVolume"`
	TextLayers    []EditTextLayer `json:"textLayers"`
}
type ExportOutput struct {
	Format      string `json:"format"`
	Resolution  string `json:"resolution"`
	AspectRatio string `json:"aspectRatio"`
}
