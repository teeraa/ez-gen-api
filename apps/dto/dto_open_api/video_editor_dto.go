package dto_open_api

import "github.com/ez-gen/ez-gen-api/apps/models"

type VideoEditorFrameDTO struct {
	Time float64 `json:"time"`
	URL  string  `json:"url"`
}
type VideoEditorAssetDTO struct {
	ID             string                `json:"id"`
	ProjectID      string                `json:"projectId"`
	ClientUploadID string                `json:"clientUploadId"`
	Name           string                `json:"name"`
	Kind           string                `json:"kind"`
	Status         string                `json:"status"`
	Duration       float64               `json:"duration"`
	Width          int                   `json:"width"`
	Height         int                   `json:"height"`
	HasAudio       bool                  `json:"hasAudio"`
	SizeBytes      int64                 `json:"sizeBytes"`
	PreviewURL     *string               `json:"previewUrl"`
	Frames         []VideoEditorFrameDTO `json:"frames"`
	ExpiresAt      *string               `json:"expiresAt"`
	FailureCode    string                `json:"failureCode"`
	FailureMessage string                `json:"failureMessage"`
}
type VideoEditorProjectDTO struct {
	ID          string                         `json:"id"`
	Title       string                         `json:"title"`
	AspectRatio string                         `json:"aspectRatio"`
	Revision    int                            `json:"revision"`
	CreatedAt   string                         `json:"createdAt"`
	UpdatedAt   string                         `json:"updatedAt"`
	Segments    []models.VideoEditorSegment    `json:"segments"`
	AudioClips  []models.VideoEditorAudioClip  `json:"audioClips"`
	TextLayers  []models.VideoEditorTextLayer  `json:"textLayers"`
	ImageLayers []models.VideoEditorImageLayer `json:"imageLayers"`
	Assets      []VideoEditorAssetDTO          `json:"assets"`
}
type VideoEditorProjectListDTO struct {
	Items []VideoEditorProjectDTO `json:"items"`
	Total int64                   `json:"total"`
	Page  int                     `json:"page"`
	Limit int                     `json:"limit"`
}
type VideoEditorExportDTO struct {
	ID             string  `json:"id"`
	ProjectID      string  `json:"projectId"`
	Revision       int     `json:"revision"`
	Status         string  `json:"status"`
	Resolution     string  `json:"resolution"`
	AspectRatio    string  `json:"aspectRatio"`
	Duration       float64 `json:"duration"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
	FailureCode    string  `json:"failureCode"`
	FailureMessage string  `json:"failureMessage"`
	CanRetry       bool    `json:"canRetry"`
}
type VideoEditorCapabilitiesDTO struct {
	Available         bool     `json:"available"`
	UnavailableReason string   `json:"unavailableReason"`
	MaxUploadBytes    int64    `json:"maxUploadBytes"`
	MaxSegments       int      `json:"maxSegments"`
	MaxDuration       float64  `json:"maxDuration"`
	AspectRatios      []string `json:"aspectRatios"`
	Resolutions       []string `json:"resolutions"`
	MaxAudioClips     int      `json:"maxAudioClips"`
	MaxTextLayers     int      `json:"maxTextLayers"`
	TextTransforms    bool     `json:"textTransforms"`
	TextOverlays      bool     `json:"textOverlays"`
	AudioFormats      []string `json:"audioFormats"`
	// ClipEffects: segments accept speed/fadeIn/fadeOut, audio clips accept fades and may use a video asset's sound.
	ClipEffects bool `json:"clipEffects"`
	// ClipStyles: segments accept colour/frame looks and transitions; text layers accept outline, box and animation.
	ClipStyles bool `json:"clipStyles"`
	// ImageLayers is true once migration 028 is applied; images are PNG/JPEG/WebP stills.
	ImageLayers    bool     `json:"imageLayers"`
	MaxImageLayers int      `json:"maxImageLayers"`
	ImageFormats   []string `json:"imageFormats"`
}
type VideoEditorAssetsDTO struct {
	Items []VideoEditorAssetDTO `json:"items"`
}
type VideoEditorExportsDTO struct {
	Items []VideoEditorExportDTO `json:"items"`
}
type VideoEditorDownloadDTO struct {
	DownloadURL string `json:"downloadUrl"`
	ExpiresAt   string `json:"expiresAt"`
	Filename    string `json:"filename"`
}
