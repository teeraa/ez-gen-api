package request_open_api

import "github.com/ez-gen/ez-gen-api/apps/models"

type CreateVideoEditorAIJob struct {
	AutoCompose    bool     `json:"autoCompose,omitempty"`
	RequestID      string   `json:"requestId"`
	Revision       int      `json:"revision"`
	AssetIDs       []string `json:"assetIds"`
	Brief          string   `json:"brief"`
	TargetDuration float64  `json:"targetDuration"`
	Style          string   `json:"style"`
	AudioMode      string   `json:"audioMode"`
	RemoveSilence  bool     `json:"removeSilence"`
	CloudConsent   bool     `json:"cloudConsent"`
}
type SelectVideoEditorAIConcept struct {
	ConceptID string `json:"conceptId"`
}
type UpdateVideoEditorAIDraft struct {
	DraftRevision int                           `json:"draftRevision"`
	Segments      []models.EditorAIDraftSegment `json:"segments"`
}
type ApplyVideoEditorAIDraft struct {
	RequestID          string   `json:"requestId"`
	Revision           int      `json:"revision"`
	DraftRevision      int      `json:"draftRevision"`
	ApprovedSegmentIDs []string `json:"approvedSegmentIds"`
}
type RetryVideoEditorAIJob struct {
	RequestID string `json:"requestId"`
}
