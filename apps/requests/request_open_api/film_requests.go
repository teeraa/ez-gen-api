package request_open_api

// Film (สร้างหนัง) requests. Optional fields are pointers so PATCH can tell "absent" from "empty".

type FilmElementRequest struct {
	IdempotencyKey string  `json:"idempotencyKey,omitempty"`
	Revision       *int    `json:"revision,omitempty"`
	Kind           *string `json:"kind,omitempty"`
	Name           *string `json:"name,omitempty"`
	Spec           *string `json:"spec,omitempty"`
	AssetID        *string `json:"assetId,omitempty"`
	// Voice (characters only): same shape as the library; omitted on create copies the asset's voice.
	Voice *CharacterVoiceRequest `json:"voice,omitempty"`
}

// FilmElementImageRequest mode: "generate" makes a new reference image; "prepare" only builds the
// faceless costume reference of a realistic/cinematic character from its existing image.
type FilmElementImageRequest struct {
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	Mode           string `json:"mode,omitempty"`
}

type FilmLineRequest struct {
	SpeakerID string `json:"speakerId"`
	Line      string `json:"line"`
	Delivery  string `json:"delivery,omitempty"`
}

type FilmShotRequest struct {
	ID           string            `json:"id,omitempty"`
	Seconds      int               `json:"seconds"`
	Transition   string            `json:"transition"`
	CharacterIDs []string          `json:"characterIds"`
	LocationID   string            `json:"locationId,omitempty"`
	PropIDs      []string          `json:"propIds"`
	Action       string            `json:"action"`
	Camera       string            `json:"camera"`
	Dialogue     []FilmLineRequest `json:"dialogue"`
	Audio        string            `json:"audio,omitempty"`
}

type FilmShotsRequest struct {
	IdempotencyKey string            `json:"idempotencyKey,omitempty"`
	Revision       int               `json:"revision"`
	Shots          []FilmShotRequest `json:"shots"`
}

type FilmVideoRequest struct {
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	ModelID        string `json:"modelId,omitempty"`
	// Draft renders a cheap 480p preview; Finalize turns the shot's current draft into the full clip.
	Draft    bool `json:"draft,omitempty"`
	Finalize bool `json:"finalize,omitempty"`
}

// FilmPlanRequest asks the chat assistant to plan the whole episode, or to rewrite one shot when ShotID is set.
type FilmPlanRequest struct {
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	Prompt         string `json:"prompt"`
	TargetSeconds  int    `json:"targetSeconds,omitempty"`
	ShotID         string `json:"shotId,omitempty"`
}

// FilmExportRequest merges the episode's finished clips into one film.
type FilmExportRequest struct {
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	Subtitles      bool   `json:"subtitles,omitempty"`
}
