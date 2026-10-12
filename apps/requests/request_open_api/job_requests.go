package request_open_api

// CreateJobRequest is the body for POST /open/v1/jobs.
type CreateJobRequest struct {
	Prompt             string   `json:"prompt"             validate:"required,min=1,max=2000"`
	Mode               string   `json:"mode"               validate:"required,oneof=text image_video image_image"`
	Model              string   `json:"model"              validate:"required"`
	AspectRatio        string   `json:"aspectRatio"        validate:"required,oneof=16:9 9:16 1:1 4:3 3:4 21:9"`
	Duration           int      `json:"duration"           validate:"required,min=4,max=30"`
	Quality            string   `json:"quality"            validate:"required,oneof=standard high"`
	EnhancePrompt      bool     `json:"enhancePrompt"`
	ReferenceImageID   *string  `json:"referenceImageId"   validate:"omitempty,uuid4"`
	SceneID            *string  `json:"sceneId"            validate:"omitempty,uuid4"`
	CharacterProfileID *string  `json:"characterProfileId" validate:"omitempty,uuid4"`
	StyleProfileID     *string  `json:"styleProfileId"     validate:"omitempty,uuid4"`
	TagIDs             []string `json:"tagIds"             validate:"omitempty,dive,uuid4"`
	IdempotencyKey     *string  `json:"idempotencyKey"     validate:"omitempty,uuid4"`
}

// ContinueSceneRequest is the body for POST /open/v1/dna/scenes/:id/continue.
type ContinueSceneRequest struct {
	Title           string  `json:"title"           validate:"max=200"`
	ActionOverride  string  `json:"actionOverride"  validate:"required,min=1,max=2000"`
	CameraOverride  string  `json:"cameraOverride"  validate:"max=200"`
	SettingOverride string  `json:"settingOverride" validate:"max=300"`
	Model           string  `json:"model"           validate:"required"`
	AspectRatio     string  `json:"aspectRatio"     validate:"required,oneof=16:9 9:16 1:1 4:3 21:9"`
	Duration        int     `json:"duration"        validate:"required,min=4,max=30"`
	Quality         string  `json:"quality"         validate:"required,oneof=standard high"`
	UseLastFrame    bool    `json:"useLastFrame"`
	IdempotencyKey  *string `json:"idempotencyKey"  validate:"omitempty,uuid4"`
}
