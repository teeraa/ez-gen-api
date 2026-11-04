package dto_open_api

type PipelineDTO struct {
	DraftExpiresAt             *string `json:"draftExpiresAt"`
	FinalUnavailableReason     *string `json:"finalUnavailableReason"`
	IsDraft                    bool    `json:"isDraft"`
	ModelID                    string  `json:"modelId"`
	ModelLabel                 string  `json:"modelLabel"`
	AspectRatio                string  `json:"aspectRatio"`
	DurationSeconds            int     `json:"durationSeconds"`
	Quality                    string  `json:"quality"`
	CanCreateFinal             bool    `json:"canCreateFinal"`
	SourceDraftID              *string `json:"sourceDraftId"`
	SourceMode                 string  `json:"sourceMode"`
	CharacterPrompt            string  `json:"characterPrompt"`
	ScenePrompt                string  `json:"scenePrompt"`
	Dialogue                   string  `json:"dialogue"`
	ID                         string  `json:"id"`
	Status                     string  `json:"status"`
	Stage                      string  `json:"stage"`
	CanRetry                   bool    `json:"canRetry"`
	FailureCode                *string `json:"failureCode"`
	FailureMessage             *string `json:"failureMessage"`
	CharacterJobID             *string `json:"characterJobId"`
	CharacterAssetID           *string `json:"characterAssetId"`
	VideoJobID                 *string `json:"videoJobId"`
	VideoAssetID               *string `json:"videoAssetId"`
	CharacterMediaURL          *string `json:"characterMediaUrl"`
	CharacterMediaURLExpiresAt *string `json:"characterMediaUrlExpiresAt"`
	VideoMediaURL              *string `json:"videoMediaUrl"`
	VideoMediaURLExpiresAt     *string `json:"videoMediaUrlExpiresAt"`
	CreatedAt                  string  `json:"createdAt"`
	UpdatedAt                  string  `json:"updatedAt"`
}
type PipelineListDTO struct {
	Items []PipelineDTO `json:"items"`
	Total int           `json:"total"`
	Page  int           `json:"page"`
	Limit int           `json:"limit"`
}
type PipelineModelCapabilitiesDTO struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	SupportsDraft bool   `json:"supportsDraft"`
	PipelineCapabilitiesDTO
}

type PipelineCapabilitiesDTO struct {
	Models              []PipelineModelCapabilitiesDTO `json:"models,omitempty"`
	LibraryAspectRatios []string                       `json:"libraryAspectRatios"`
	LibraryDurations    []int                          `json:"libraryDurations"`
	TextEnabled         bool                           `json:"textEnabled"`
	LibraryEnabled      bool                           `json:"libraryEnabled"`
	Enabled             bool                           `json:"enabled"`
	SupportsDialogue    bool                           `json:"supportsDialogue"`
	UnavailableReason   *string                        `json:"unavailableReason"`
	AspectRatios        []string                       `json:"aspectRatios"`
	Durations           []int                          `json:"durations"`
}

// PipelinePreflightDTO checks a pipeline request without creating anything, so the studio can
// block inputs the create endpoint would reject (lengths, model support, prompt budget).
type PipelinePreflightDTO struct {
	OK bool `json:"ok"`
	// PromptRunes counts the compiled prompt: hidden controls, scene and the spoken-dialogue
	// instruction. With dialogue it must not exceed PromptLimit.
	PromptRunes int `json:"promptRunes"`
	PromptLimit int `json:"promptLimit"`
	// SceneTruncated: without dialogue an overlong prompt is cut, not rejected.
	SceneTruncated bool                     `json:"sceneTruncated"`
	Issues         []PipelinePreflightIssue `json:"issues"`
}

type PipelinePreflightIssue struct {
	Field   string `json:"field"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}
