package request_open_api

type CreatePipelineRequest struct {
	Draft            bool   `json:"draft,omitempty"`
	ReferenceImageID string `json:"referenceImageId" validate:"omitempty,uuid4"`
	CharacterAssetID string `json:"characterAssetId,omitempty" validate:"omitempty,uuid"`
	CharacterPrompt  string `json:"characterPrompt" validate:"max=2000"`
	ScenePrompt      string `json:"scenePrompt" validate:"required,min=1,max=2000"`
	Dialogue         string `json:"dialogue" validate:"max=1000"`
	VisualStyle      string `json:"visualStyle" validate:"omitempty,oneof=cinematic_realistic 3d_animation anime illustration clay"`
	AspectRatio      string `json:"aspectRatio" validate:"omitempty,oneof=16:9 9:16 1:1 4:3 3:4"`
	DurationSeconds  int    `json:"durationSeconds" validate:"omitempty,min=4,max=15"`
	ModelID          string `json:"modelId" validate:"omitempty,oneof=violet-motion-2 violet-cinema violet-fast violet-realism violet-classic"`
	Quality          string `json:"quality" validate:"omitempty,oneof=standard high"`
	IdempotencyKey   string `json:"idempotencyKey" validate:"required,uuid4"`
}

type FinalizePipelineRequest struct {
	IdempotencyKey string `json:"idempotencyKey" validate:"required,uuid4"`
}
