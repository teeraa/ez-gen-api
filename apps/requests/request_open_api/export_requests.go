package request_open_api

// CreateExportRequest is the body for POST /open/v1/exports.
type CreateExportRequest struct {
	// SceneIDs lists the scenes to merge, in presentation order (min 2, max 20).
	SceneIDs []string `json:"sceneIds" validate:"required,min=2,max=20,dive,uuid4"`
	// AudioUploadID references an Upload record (audio file) created via POST /open/v1/uploads.
	AudioUploadID *string  `json:"audioUploadId" validate:"omitempty,uuid4"`
	AudioVolume   *float64 `json:"audioVolume"`
	// TextLayers adds per-scene caption overlays.
	TextLayers []TextLayerInput `json:"textLayers" validate:"omitempty,dive"`
}

// TextLayerInput is a single caption overlay attached to one scene.
type TextLayerInput struct {
	SceneID  string  `json:"sceneId"  validate:"required,uuid4"`
	Text     string  `json:"text"     validate:"required,min=1,max=200"`
	Position string  `json:"position" validate:"omitempty,oneof=top center bottom"`
	FontSize *int    `json:"fontSize"`
	Color    *string `json:"color"`
}
