package request_open_api

type AuthRequest struct {
	Email       string `json:"email" validate:"required,email,max=254"`
	Password    string `json:"password" validate:"required,min=10,max=72"`
	DisplayName string `json:"displayName" validate:"max=100"`
}
type CreateAssetRequest struct {
	Name        string  `json:"name" validate:"required,min=1,max=100"`
	AssetType   string  `json:"assetType" validate:"required,oneof=character location prop costume style"`
	Description string  `json:"description" validate:"max=2000"`
	Prompt      string  `json:"prompt" validate:"max=2000"`
	Scope       string  `json:"scope" validate:"required,oneof=personal"`
	JobID       *string `json:"jobId" validate:"omitempty,uuid4"`
	UploadID    *string `json:"uploadId" validate:"omitempty,uuid4"`
}
type UpdateAssetRequest struct {
	Name        *string `json:"name" validate:"omitempty,min=1,max=100"`
	Description *string `json:"description" validate:"omitempty,max=2000"`
}
type CreateImageRequest struct {
	Category         string  `json:"category" validate:"omitempty,oneof=character product"`
	VisualStyle      string  `json:"visualStyle" validate:"omitempty,oneof=auto cinematic_realistic 3d_animation anime illustration clay"`
	Mood             string  `json:"mood" validate:"omitempty,oneof=neutral warm confident mysterious serious energetic"`
	DesignFormat     string  `json:"designFormat" validate:"omitempty,oneof=full_body turnaround portrait"`
	Prompt           string  `json:"prompt" validate:"required,min=1,max=2000"`
	AspectRatio      string  `json:"aspectRatio" validate:"required,oneof=1:1 3:4 4:3 16:9 9:16"`
	ReferenceImageID *string `json:"referenceImageId" validate:"omitempty,uuid4"`
	IdempotencyKey   string  `json:"idempotencyKey" validate:"required,uuid4"`
}
