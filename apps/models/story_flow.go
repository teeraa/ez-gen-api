package models

import (
	"github.com/google/uuid"
	"time"
)

// A separate aggregate keeps Story Flow revisions out of legacy scene/job semantics.
type StoryFlowProject struct {
	Base
	UserID         uuid.UUID `gorm:"type:uuid;not null"`
	IdempotencyKey string
	RequestHash    string
	Document       []byte `gorm:"type:jsonb"`
	Status         string
	LeaseToken     *uuid.UUID `gorm:"type:uuid"`
	LeaseUntil     *time.Time
}

func (StoryFlowProject) TableName() string { return "story_flow_projects" }

type StoryFlowContinuity struct {
	Wardrobe       string `json:"wardrobe"`
	Lighting       string `json:"lighting"`
	CharacterState string `json:"characterState"`
}
type StoryFlowCharacter struct {
	AppearanceExcerpt string `json:"appearanceExcerpt"`
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	StorageKey        string `json:"storageKey"`
}
type StoryFlowShot struct {
	PrimaryCharacterID  string              `json:"primaryCharacterId"`
	ProviderTaskHistory []string            `json:"providerTaskHistory"`
	ID                  string              `json:"id"`
	SceneIndex          int                 `json:"sceneIndex"`
	ShotIndex           int                 `json:"shotIndex"`
	Action              string              `json:"action"`
	Characters          []string            `json:"characters"`
	Location            string              `json:"location"`
	Atmosphere          string              `json:"atmosphere"`
	Framing             string              `json:"framing"`
	CameraAngle         string              `json:"cameraAngle"`
	CameraMovement      string              `json:"cameraMovement"`
	Continuity          StoryFlowContinuity `json:"continuity"`
	ImagePrompt         string              `json:"imagePrompt"`
	VideoPrompt         string              `json:"videoPrompt"`
	DurationSeconds     int                 `json:"durationSeconds"`
	ImageStatus         string              `json:"imageStatus"`
	VideoStatus         string              `json:"videoStatus"`
	ImageKey            string              `json:"imageKey"`
	VideoKey            string              `json:"videoKey"`
	ProviderTaskID      string              `json:"providerTaskId"`
	FailureCode         string              `json:"failureCode"`
	FailureMessage      string              `json:"failureMessage"`
	CanRetry            bool                `json:"canRetry"`
}
type StoryFlowScene struct {
	SceneIndex int                 `json:"sceneIndex"`
	Title      string              `json:"title"`
	Narrative  string              `json:"narrative"`
	Location   string              `json:"location"`
	Characters []string            `json:"characters"`
	Emotion    string              `json:"emotion"`
	Continuity StoryFlowContinuity `json:"continuity"`
	Shots      []StoryFlowShot     `json:"shots"`
}
