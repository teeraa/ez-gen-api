package request_open_api

import "github.com/ez-gen/ez-gen-api/apps/models"

type CreateVideoEditorProject struct {
	RequestID string `json:"requestId" validate:"required,uuid"`
	Title     string `json:"title" validate:"required,min=1,max=200"`
}
type SaveVideoEditorProject struct {
	Revision    int                         `json:"revision" validate:"required,min=1"`
	Title       string                      `json:"title" validate:"required,min=1,max=200"`
	AspectRatio string                      `json:"aspectRatio" validate:"required,oneof=9:16 16:9 1:1"`
	Segments    []models.VideoEditorSegment `json:"segments" validate:"max=20"`
	// Omitted (or null) keeps the stored value; an array replaces it.
	AudioClips *[]models.VideoEditorAudioClip `json:"audioClips,omitempty" validate:"omitempty,max=20"`
	TextLayers *[]models.VideoEditorTextLayer `json:"textLayers,omitempty" validate:"omitempty,max=50"`
	// Requires migration 028; omitted keeps the stored value.
	ImageLayers *[]models.VideoEditorImageLayer `json:"imageLayers,omitempty" validate:"omitempty,max=20"`
}
type CreateVideoEditorExport struct {
	RequestID  string `json:"requestId" validate:"required,uuid"`
	Revision   int    `json:"revision" validate:"required,min=1"`
	Resolution string `json:"resolution" validate:"required,oneof=720p 1080p"`
}
