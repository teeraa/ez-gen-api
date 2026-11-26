package request_open_api

// Film (ภาพยนตร์) requests. Optional fields are pointers so PATCH can tell "absent" from "empty".

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
	// Emotion is a Voice-menu emotion for this line ("" = the speaker's voice setting).
	Emotion string `json:"emotion,omitempty"`
}

// FilmCutRequest is one shot inside a multi-shot clip: its own camera and one clear action.
type FilmCutRequest struct {
	Camera string `json:"camera"`
	Action string `json:"action"`
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
	// Blocking: each visible character's frame side, posture and facing (English).
	Blocking string `json:"blocking,omitempty"`
	// Cuts make the clip a multi-shot sequence: the video model renders them with hard cuts
	// inside the one clip (2-8 cuts; empty = one continuous shot).
	Cuts []FilmCutRequest `json:"cuts,omitempty"`
	// TrimIn/TrimOut cut seconds from the start/end of the shot's clip in the studio and the export.
	TrimIn  float64 `json:"trimIn,omitempty"`
	TrimOut float64 `json:"trimOut,omitempty"`
	// LineAt moves the clip's dubbed lines (order of video.lines), seconds from the clip start.
	LineAt []float64 `json:"lineAt,omitempty"`
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
	// Speech "native" (experimental): the provider speaks the Thai lines itself with each speaker's
	// voice as a timbre sample, so lips match; "" or "dub" overlays the generated lines (default).
	Speech string `json:"speech,omitempty"`
}

// FilmPlanRequest asks the chat assistant to plan the whole episode, or to rewrite one shot when ShotID is set.
type FilmPlanRequest struct {
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	Prompt         string `json:"prompt"`
	TargetSeconds  int    `json:"targetSeconds,omitempty"`
	ShotID         string `json:"shotId,omitempty"`
	// Scope is what ShotID selects: "shot" (default) or "scene" (the shot's whole scene).
	Scope string `json:"scope,omitempty"`
}

// FilmPlanConfirmRequest accepts or drops the episode's pending assistant edit.
type FilmPlanConfirmRequest struct {
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	PendingID      string `json:"pendingId"`
	Accept         bool   `json:"accept"`
}

// FilmMusicRequest changes the episode's music track: its volume (0..1) or, with Remove, removes it.
// The file itself is uploaded as multipart to the same path.
type FilmMusicRequest struct {
	IdempotencyKey string   `json:"idempotencyKey,omitempty"`
	Volume         *float64 `json:"volume,omitempty"`
	// Start places the music on the film timeline, in seconds.
	Start *float64 `json:"start,omitempty"`
	// TrimIn skips the song's first seconds; Length cuts how long it plays (0 = to the song's end).
	TrimIn *float64 `json:"trimIn,omitempty"`
	Length *float64 `json:"length,omitempty"`
	Remove bool     `json:"remove,omitempty"`
}

// FilmRedubRequest re-dubs every finished clip whose characters' voices changed.
type FilmRedubRequest struct {
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
}

// FilmExportRequest merges the episode's finished clips into one film.
type FilmExportRequest struct {
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	Subtitles      bool   `json:"subtitles,omitempty"`
}

// FilmSkillsRequest replaces the episode's chosen film skills (Skills menu, output "film"); [] clears them.
type FilmSkillsRequest struct {
	IdempotencyKey string   `json:"idempotencyKey,omitempty"`
	SkillIDs       []string `json:"skillIds"`
}
