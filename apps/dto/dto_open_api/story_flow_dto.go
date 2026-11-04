package dto_open_api

import (
	"encoding/json"
	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

type StoryFlowModelDTO struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	AspectRatios []string `json:"aspectRatios"`
	Qualities    []string `json:"qualities"`
	MinDuration  int      `json:"minDuration"`
	MaxDuration  int      `json:"maxDuration"`
}
type StoryFlowCapabilitiesDTO struct {
	Enabled           bool                `json:"enabled"`
	UnavailableReason string              `json:"unavailableReason"`
	Models            []StoryFlowModelDTO `json:"models"`
	Styles            []string            `json:"styles"`
	MaxScenes         int                 `json:"maxScenes"`
	MaxShots          int                 `json:"maxShots"`
}

// Snapshot is persisted only; public views explicitly omit provider identifiers and object keys.
type StoryFlowSnapshot struct {
	AnalysisStatuses         map[string]string           `json:"analysisStatuses"`
	AnalysisPrompts          map[string]string           `json:"analysisPrompts"`
	CharacterCompilerVersion string                      `json:"characterCompilerVersion"`
	Input                    req.CreateStoryFlowRequest  `json:"input"`
	Model                    modelark.ModelConfig        `json:"model"`
	AnalysisModel            string                      `json:"analysisModel"`
	ImageModel               string                      `json:"imageModel"`
	PromptVersion            string                      `json:"promptVersion"`
	Characters               []models.StoryFlowCharacter `json:"characters"`
	Scenes                   []models.StoryFlowScene     `json:"scenes"`
	SceneShots               []req.StoryFlowSceneShots   `json:"sceneShots"`
	Revision                 int                         `json:"revision"`
	Status                   string                      `json:"status"`
	Stage                    string                      `json:"stage"`
	FailureCode              string                      `json:"failureCode"`
	FailureMessage           string                      `json:"failureMessage"`
	CanRetry                 bool                        `json:"canRetry"`
	OutputKey                string                      `json:"outputKey"`
	Operations               map[string]string           `json:"operations"`
	AnalysisPrompt           string                      `json:"analysisPrompt"`
	CharacterStatus          string                      `json:"characterStatus"`
	CharacterPrompt          string                      `json:"characterPrompt"`
	ExportStatus             string                      `json:"exportStatus"`
}
type StoryFlowShotDTO struct {
	ID              string                     `json:"id"`
	SceneIndex      int                        `json:"sceneIndex"`
	ShotIndex       int                        `json:"shotIndex"`
	Action          string                     `json:"action"`
	Characters      []string                   `json:"characters"`
	Location        string                     `json:"location"`
	Atmosphere      string                     `json:"atmosphere"`
	Framing         string                     `json:"framing"`
	CameraAngle     string                     `json:"cameraAngle"`
	CameraMovement  string                     `json:"cameraMovement"`
	Continuity      models.StoryFlowContinuity `json:"continuity"`
	ImagePrompt     string                     `json:"imagePrompt"`
	VideoPrompt     string                     `json:"videoPrompt"`
	DurationSeconds int                        `json:"durationSeconds"`
	ImageStatus     string                     `json:"imageStatus"`
	VideoStatus     string                     `json:"videoStatus"`
	ImageURL        *string                    `json:"imageUrl"`
	VideoURL        *string                    `json:"videoUrl"`
	FailureCode     string                     `json:"failureCode"`
	FailureMessage  string                     `json:"failureMessage"`
	CanRetry        bool                       `json:"canRetry"`
}
type StoryFlowSceneDTO struct {
	SceneIndex int                        `json:"sceneIndex"`
	Title      string                     `json:"title"`
	Narrative  string                     `json:"narrative"`
	Location   string                     `json:"location"`
	Characters []string                   `json:"characters"`
	Emotion    string                     `json:"emotion"`
	Continuity models.StoryFlowContinuity `json:"continuity"`
	Shots      []StoryFlowShotDTO         `json:"shots"`
}
type StoryFlowCharacterSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type StoryFlowProjectDTO struct {
	CharacterSummaries []StoryFlowCharacterSummary `json:"characterSummaries"`
	ID                 string                      `json:"id"`
	req.CreateStoryFlowRequest
	Revision        int                 `json:"revision"`
	Status          string              `json:"status"`
	Stage           string              `json:"stage"`
	Scenes          []StoryFlowSceneDTO `json:"scenes"`
	OutputURL       *string             `json:"outputUrl"`
	DownloadURL     *string             `json:"downloadUrl"`
	FailureCode     string              `json:"failureCode"`
	FailureMessage  string              `json:"failureMessage"`
	CanRetry        bool                `json:"canRetry"`
	CompletedImages int                 `json:"completedImages"`
	CompletedVideos int                 `json:"completedVideos"`
	TotalShots      int                 `json:"totalShots"`
	CreatedAt       string              `json:"createdAt"`
	UpdatedAt       string              `json:"updatedAt"`
}
type StoryFlowListDTO struct {
	Items []StoryFlowProjectDTO `json:"items"`
	Total int64                 `json:"total"`
	Page  int                   `json:"page"`
	Limit int                   `json:"limit"`
}

func DecodeStoryFlow(p *models.StoryFlowProject) (StoryFlowSnapshot, error) {
	var s StoryFlowSnapshot
	e := json.Unmarshal(p.Document, &s)
	return s, e
}
