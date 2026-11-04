package dto_open_api

import "github.com/ez-gen/ez-gen-api/apps/models"

type EditorAIProgressDTO struct {
	CompletedAssets int `json:"completedAssets"`
	TotalAssets     int `json:"totalAssets"`
}
type EditorAISupportedOptions struct {
	RemoveSilence bool `json:"removeSilence"`
	Filler        bool `json:"filler"`
	Shaky         bool `json:"shaky"`
	Subtitles     bool `json:"subtitles"`
	Music         bool `json:"music"`
}
type VideoEditorAICapabilitiesDTO struct {
	AutoCompose            bool                     `json:"autoCompose"`
	Available              bool                     `json:"available"`
	UnavailableReason      string                   `json:"unavailableReason"`
	TranscriptionAvailable bool                     `json:"transcriptionAvailable"`
	AnalysisAvailable      bool                     `json:"analysisAvailable"`
	MaxAssets              int                      `json:"maxAssets"`
	MaxSourceDuration      float64                  `json:"maxSourceDuration"`
	MaxFrames              int                      `json:"maxFrames"`
	MaxTargetDuration      float64                  `json:"maxTargetDuration"`
	MaxSegments            int                      `json:"maxSegments"`
	MaxJobsPerDay          int                      `json:"maxJobsPerDay"`
	MaxCloudCalls          int                      `json:"maxCloudCalls"`
	CloudNotice            string                   `json:"cloudNotice"`
	ConnectionMode         string                   `json:"connectionMode"`
	SupportedOptions       EditorAISupportedOptions `json:"supportedOptions"`
}
type VideoEditorAIJobDTO struct {
	AutoCompose       bool                        `json:"autoCompose"`
	ID                string                      `json:"id"`
	ProjectID         string                      `json:"projectId"`
	SourceRevision    int                         `json:"sourceRevision"`
	Phase             string                      `json:"phase"`
	Status            string                      `json:"status"`
	Stage             string                      `json:"stage"`
	AssetIDs          []string                    `json:"assetIds"`
	Brief             string                      `json:"brief"`
	TargetDuration    float64                     `json:"targetDuration"`
	Style             string                      `json:"style"`
	AudioMode         string                      `json:"audioMode"`
	RemoveSilence     bool                        `json:"removeSilence"`
	Progress          EditorAIProgressDTO         `json:"progress"`
	Transcripts       []models.EditorAITranscript `json:"transcripts"`
	Visuals           []models.EditorAIVisual     `json:"visuals"`
	Concepts          []models.EditorAIConcept    `json:"concepts"`
	SelectedConceptID *string                     `json:"selectedConceptId"`
	Draft             *models.EditorAIDraft       `json:"draft"`
	FailureCode       string                      `json:"failureCode"`
	FailureMessage    string                      `json:"failureMessage"`
	CanRetry          bool                        `json:"canRetry"`
	RetryAt           *string                     `json:"retryAt"`
	CloudCallsUsed    int                         `json:"cloudCallsUsed"`
	CloudCallsLimit   int                         `json:"cloudCallsLimit"`
	CreatedAt         string                      `json:"createdAt"`
	UpdatedAt         string                      `json:"updatedAt"`
	AppliedRevision   *int                        `json:"appliedRevision"`
}
type VideoEditorAIJobsDTO struct {
	Items []VideoEditorAIJobDTO `json:"items"`
}
