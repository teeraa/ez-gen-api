package request_open_api

// CreateExportRequest is the body for POST /open/v1/exports.
// Either sceneIds or clips must resolve to 2–20 scenes; clips supersedes sceneIds.
type CreateExportRequest struct {
	// SceneIDs is the legacy ordered scene list (no trim).
	SceneIDs []string `json:"sceneIds" validate:"omitempty,max=20,dive,uuid4"`
	// Clips supersedes sceneIds and adds per-clip trim.
	Clips []ClipInput `json:"clips" validate:"omitempty,max=20,dive"`

	AudioUploadID *string  `json:"audioUploadId" validate:"omitempty,uuid4"`
	AudioVolume   *float64 `json:"audioVolume"`

	TextLayers []TextLayerInput `json:"textLayers" validate:"omitempty,dive"`
}

// ClipInput is one scene clip with an optional trim window (seconds from clip start).
type ClipInput struct {
	SceneID   string   `json:"sceneId"   validate:"required,uuid4"`
	TrimStart *float64 `json:"trimStart" validate:"omitempty,min=0"`
	TrimEnd   *float64 `json:"trimEnd"   validate:"omitempty,min=0"`
}

// TextLayerInput is a single caption overlay attached to one scene.
type TextLayerInput struct {
	SceneID  string  `json:"sceneId"  validate:"required,uuid4"`
	Text     string  `json:"text"     validate:"required,min=1,max=200"`
	Position string  `json:"position" validate:"omitempty,oneof=top center bottom"`
	FontSize *int    `json:"fontSize"`
	Color    *string `json:"color"`

	// Free placement (timeline editor). Normalized 0..1 of the frame, centre anchor.
	X     *float64 `json:"x"     validate:"omitempty,min=0,max=1"`
	Y     *float64 `json:"y"     validate:"omitempty,min=0,max=1"`
	Align string   `json:"align" validate:"omitempty,oneof=left center right"`

	// Timing in seconds relative to the clip's trimmed start; both absent = whole clip.
	StartTime *float64 `json:"startTime" validate:"omitempty,min=0"`
	EndTime   *float64 `json:"endTime"   validate:"omitempty,min=0"`
}
