package models

import (
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"time"
)

type EditorAIWord struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}
type EditorAISpeech struct {
	Start float64        `json:"start"`
	End   float64        `json:"end"`
	Text  string         `json:"text"`
	Words []EditorAIWord `json:"words"`
}
type EditorAIRefinedSegment struct {
	SegmentIndex int    `json:"segmentIndex"`
	Text         string `json:"text"`
	Uncertain    bool   `json:"uncertain"`
}
type EditorAIRefinement struct {
	Text     string                   `json:"text"`
	Segments []EditorAIRefinedSegment `json:"segments"`
}
type EditorAITranscript struct {
	AssetID    string              `json:"assetId"`
	Language   string              `json:"language"`
	Text       string              `json:"text"`
	HasSpeech  bool                `json:"hasSpeech"`
	Segments   []EditorAISpeech    `json:"segments"`
	Refinement *EditorAIRefinement `json:"refinement,omitempty"`
}
type EditorAIVisualFrame struct {
	Time        float64 `json:"time"`
	Description string  `json:"description"`
}
type EditorAIVisual struct {
	AssetID string                `json:"assetId"`
	Summary string                `json:"summary"`
	Frames  []EditorAIVisualFrame `json:"frames"`
}
type EditorAIConcept struct {
	DurationReason string  `json:"durationReason,omitempty"`
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Summary        string  `json:"summary"`
	Duration       float64 `json:"duration"`
}
type EditorAIDraftSegment struct {
	ID         string  `json:"id"`
	AssetID    string  `json:"assetId"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Volume     float64 `json:"volume"`
	Title      string  `json:"title"`
	Reason     string  `json:"reason"`
	EvidenceID string  `json:"evidenceId"`
}
type EditorAIDraft struct {
	TextLayers       []VideoEditorTextLayer `json:"textLayers,omitempty"`
	AudioClips       []VideoEditorAudioClip `json:"audioClips,omitempty"`
	CompositionNotes []string               `json:"compositionNotes,omitempty"`
	DurationReason   string                 `json:"durationReason,omitempty"`
	Revision         int                    `json:"revision"`
	Duration         float64                `json:"duration"`
	Segments         []EditorAIDraftSegment `json:"segments"`
}
type EditorAISource struct {
	ID       string  `json:"id"`
	Hash     string  `json:"hash"`
	Key      string  `json:"key"`
	Duration float64 `json:"duration"`
	HasAudio bool    `json:"hasAudio"`
}
type EditorAIInput struct {
	AutoCompose     bool             `json:"autoCompose,omitempty"`
	AudioSources    []EditorAISource `json:"audioSources,omitempty"`
	ManualAudio     bool             `json:"manualAudio,omitempty"`
	ManualTextCount int              `json:"manualTextCount,omitempty"`
	Sources         []EditorAISource `json:"sources"`
	Brief           string           `json:"brief"`
	TargetDuration  float64          `json:"targetDuration"`
	Style           string           `json:"style"`
	AudioMode       string           `json:"audioMode"`
	RemoveSilence   bool             `json:"removeSilence"`
	ModelSignature  string           `json:"modelSignature"`
	ASRSignature    string           `json:"asrSignature"`
}
type EditorAIEvidence struct {
	ID          string  `json:"id"`
	AssetID     string  `json:"assetId"`
	Start       float64 `json:"start"`
	End         float64 `json:"end"`
	Description string  `json:"description"`
}
type VideoEditorAIJob struct {
	Base
	UserID            uuid.UUID `gorm:"type:uuid;not null"`
	ProjectID         uuid.UUID `gorm:"type:uuid;not null"`
	RequestID         uuid.UUID `gorm:"type:uuid;not null"`
	RequestHash       string
	SourceRevision    int
	Phase             string
	Status            string
	Stage             string
	Input             datatypes.JSON `gorm:"type:jsonb"`
	Transcripts       datatypes.JSON `gorm:"type:jsonb"`
	Visuals           datatypes.JSON `gorm:"type:jsonb"`
	Concepts          datatypes.JSON `gorm:"type:jsonb"`
	Evidence          datatypes.JSON `gorm:"type:jsonb"`
	Draft             datatypes.JSON `gorm:"type:jsonb"`
	SelectedConceptID *uuid.UUID     `gorm:"type:uuid"`
	CompletedAssets   int
	AnalysisAttempts  int
	PlanningAttempts  int
	CloudCallsUsed    int
	AttemptToken      *uuid.UUID `gorm:"type:uuid"`
	LeaseUntil        *time.Time
	RetryAt           *time.Time
	RetryRequests     datatypes.JSON `gorm:"type:jsonb"`
	FailureCode       string
	FailureMessage    string
	AppliedRequestID  *uuid.UUID     `gorm:"type:uuid"`
	AppliedProject    datatypes.JSON `gorm:"type:jsonb"`
}

func (VideoEditorAIJob) TableName() string { return "video_editor_ai_jobs" }

type VideoEditorAICache struct {
	Base
	UserID      uuid.UUID `gorm:"type:uuid;not null"`
	ProjectID   uuid.UUID `gorm:"type:uuid;not null"`
	AssetID     uuid.UUID `gorm:"type:uuid;not null"`
	ContentHash string
	Kind        string
	Signature   string
	Result      datatypes.JSON `gorm:"type:jsonb"`
}

func (VideoEditorAICache) TableName() string { return "video_editor_ai_cache" }
