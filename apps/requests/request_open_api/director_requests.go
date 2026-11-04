package request_open_api

type DirectorRequest struct {
	Revision          *int      `json:"revision,omitempty"`
	IdempotencyKey    string    `json:"idempotencyKey,omitempty"`
	Name              *string   `json:"name,omitempty"`
	AspectRatio       *string   `json:"aspectRatio,omitempty"`
	Style             *string   `json:"style,omitempty"`
	Resolution        *string   `json:"resolution,omitempty"`
	Step              *string   `json:"step,omitempty"`
	Prompt            *string   `json:"prompt,omitempty"`
	Genre             *string   `json:"genre,omitempty"`
	Moods             *[]string `json:"moods,omitempty"`
	SceneCount        *int      `json:"sceneCount,omitempty"`
	CharacterAssetIDs *[]string `json:"characterAssetIds,omitempty"`
	StoryModelID      *string   `json:"storyModelId,omitempty"`
	ImageModelID      *string   `json:"imageModelId,omitempty"`
	VideoModelID      *string   `json:"videoModelId,omitempty"`
	ShotID            *string   `json:"shotId,omitempty"`
	Replace           bool      `json:"replace,omitempty"`
	Title             *string   `json:"title,omitempty"`
	Summary           *string   `json:"summary,omitempty"`
	ShotCount         *int      `json:"shotCount,omitempty"`
	Instruction       *string   `json:"instruction,omitempty"`
	Description       *string   `json:"description,omitempty"`
	Camera            *string   `json:"camera,omitempty"`
	AfterShotID       *string   `json:"afterShotId,omitempty"`
	ShotIDs           *[]string `json:"shotIds,omitempty"`
	Draft             bool      `json:"draft,omitempty"`
	Finalize          bool      `json:"finalize,omitempty"`
}
