package dto_open_api

import "time"

type DirectorIdea struct {
	Prompt            string   `json:"prompt"`
	Genre             string   `json:"genre"`
	Moods             []string `json:"moods"`
	SceneCount        int      `json:"sceneCount"`
	CharacterAssetIDs []string `json:"characterAssetIds"`
	StoryModelID      string   `json:"storyModelId"`
}
type DirectorFailure struct {
	FailureCode    *string `json:"failureCode"`
	FailureMessage *string `json:"failureMessage"`
	CanRetry       bool    `json:"canRetry"`
}
type DirectorCover struct {
	ID           string     `json:"id"`
	Status       string     `json:"status"`
	Progress     int        `json:"progress"`
	ImageURL     *string    `json:"imageUrl"`
	ExpiresAt    *time.Time `json:"expiresAt"`
	Source       string     `json:"source"`
	ShotID       *string    `json:"shotId"`
	ImageModelID *string    `json:"imageModelId"`
	DirectorFailure
	PointsCharged  *int `json:"pointsCharged"`
	PointsRefunded int  `json:"pointsRefunded"`
}
type DirectorStory struct {
	Status                    string  `json:"status"`
	Phase                     *string `json:"phase"`
	Logline                   *string `json:"logline"`
	StoryModelID              *string `json:"storyModelId"`
	CompletedScenes           int     `json:"completedScenes"`
	TotalScenes               int     `json:"totalScenes"`
	EstimatedSecondsRemaining *int    `json:"estimatedSecondsRemaining"`
	DirectorFailure
	PointsCharged  int `json:"pointsCharged"`
	PointsRefunded int `json:"pointsRefunded"`
}
type DirectorStoryboard struct {
	Status                    string `json:"status"`
	ImageModelID              string `json:"imageModelId"`
	VideoModelID              string `json:"videoModelId"`
	CompletedImages           int    `json:"completedImages"`
	TotalShots                int    `json:"totalShots"`
	EstimatedSecondsRemaining *int   `json:"estimatedSecondsRemaining"`
	StyleReference            string `json:"styleReference"`
}
type DirectorImage struct {
	Status    string     `json:"status"`
	Progress  int        `json:"progress"`
	URL       *string    `json:"url"`
	ExpiresAt *time.Time `json:"expiresAt"`
	Stale     bool       `json:"stale"`
	DirectorFailure
}
type DirectorVideo struct {
	Status          string     `json:"status"`
	Progress        int        `json:"progress"`
	URL             *string    `json:"url"`
	ExpiresAt       *time.Time `json:"expiresAt"`
	Stale           bool       `json:"stale"`
	ModelID         string     `json:"modelId"`
	DurationSeconds int        `json:"durationSeconds"`
	DirectorFailure
	PointsCharged  int `json:"pointsCharged"`
	PointsRefunded int `json:"pointsRefunded"`
}
type DirectorShot struct {
	ID                string         `json:"id"`
	SceneID           string         `json:"sceneId"`
	Index             int            `json:"index"`
	Description       string         `json:"description"`
	Camera            string         `json:"camera"`
	CharacterAssetIDs []string       `json:"characterAssetIds"`
	Image             DirectorImage  `json:"image"`
	Video             *DirectorVideo `json:"video"`
	PointsCharged     *int           `json:"pointsCharged"`
	PointsRefunded    int            `json:"pointsRefunded"`
}
type DirectorScene struct {
	ID                string         `json:"id"`
	Index             int            `json:"index"`
	Status            string         `json:"status"`
	Title             string         `json:"title"`
	Summary           string         `json:"summary"`
	Mood              *string        `json:"mood"`
	CharacterAssetIDs []string       `json:"characterAssetIds"`
	ShotCount         int            `json:"shotCount"`
	Shots             []DirectorShot `json:"shots"`
	DirectorFailure
	PointsRefunded int `json:"pointsRefunded"`
}
type DirectorEstimates struct {
	Story      int `json:"story"`
	Cover      int `json:"cover"`
	Storyboard int `json:"storyboard"`
	Video      int `json:"video"`
}
type DirectorProject struct {
	ID                  string             `json:"id"`
	Revision            int                `json:"revision"`
	Name                string             `json:"name"`
	AspectRatio         string             `json:"aspectRatio"`
	Style               string             `json:"style"`
	Resolution          string             `json:"resolution"`
	Step                string             `json:"step"`
	Idea                DirectorIdea       `json:"idea"`
	Cover               *DirectorCover     `json:"cover"`
	Story               DirectorStory      `json:"story"`
	Storyboard          DirectorStoryboard `json:"storyboard"`
	Scenes              []DirectorScene    `json:"scenes"`
	Estimates           DirectorEstimates  `json:"estimates"`
	CoverImageURL       *string            `json:"coverImageUrl"`
	CoverImageExpiresAt *time.Time         `json:"coverImageExpiresAt"`
	CreatedAt           time.Time          `json:"createdAt"`
	UpdatedAt           time.Time          `json:"updatedAt"`
}
type DirectorSummary struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	AspectRatio         string     `json:"aspectRatio"`
	Style               string     `json:"style"`
	Resolution          string     `json:"resolution"`
	Step                string     `json:"step"`
	StoryStatus         string     `json:"storyStatus"`
	StoryboardStatus    string     `json:"storyboardStatus"`
	SceneCount          int        `json:"sceneCount"`
	TotalShots          int        `json:"totalShots"`
	CompletedImages     int        `json:"completedImages"`
	CoverImageURL       *string    `json:"coverImageUrl"`
	CoverImageExpiresAt *time.Time `json:"coverImageExpiresAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}
type DirectorList struct {
	Items []DirectorSummary `json:"items"`
	Total int64             `json:"total"`
	Page  int               `json:"page"`
	Limit int               `json:"limit"`
}
type DirectorOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}
type DirectorStyle struct {
	DirectorOption
	SampleImageURL string `json:"sampleImageUrl"`
}
type DirectorResolution struct {
	DirectorOption
	Available bool `json:"available"`
}
type DirectorModel struct {
	ID                         string   `json:"id"`
	Name                       string   `json:"name"`
	Note                       string   `json:"note"`
	SpeedHint                  string   `json:"speedHint"`
	PointsPerUnit              int      `json:"pointsPerUnit"`
	SupportsCharacterReference bool     `json:"supportsCharacterReference"`
	Default                    bool     `json:"default,omitempty"`
	AspectRatios               []string `json:"aspectRatios,omitempty"`
}
type DirectorModels struct {
	Story []DirectorModel `json:"story"`
	Image []DirectorModel `json:"image"`
	Video []DirectorModel `json:"video"`
}
type DirectorLimits struct {
	IdeaMaxLength             int `json:"ideaMaxLength"`
	NameMaxLength             int `json:"nameMaxLength"`
	MinScenes                 int `json:"minScenes"`
	MaxScenes                 int `json:"maxScenes"`
	MinShotsPerScene          int `json:"minShotsPerScene"`
	MaxShotsPerScene          int `json:"maxShotsPerScene"`
	MaxShotsPerSceneAfterEdit int `json:"maxShotsPerSceneAfterEdit"`
	MaxTotalShots             int `json:"maxTotalShots"`
	MaxCast                   int `json:"maxCast"`
	MaxMoods                  int `json:"maxMoods"`
	SceneSummaryMaxLength     int `json:"sceneSummaryMaxLength"`
	ShotDescriptionMaxLength  int `json:"shotDescriptionMaxLength"`
}
type DirectorCapabilities struct {
	Enabled           bool                 `json:"enabled"`
	VideoEnabled      bool                 `json:"videoEnabled"`
	UnavailableReason *string              `json:"unavailableReason"`
	AspectRatios      []DirectorOption     `json:"aspectRatios"`
	Resolutions       []DirectorResolution `json:"resolutions"`
	Styles            []DirectorStyle      `json:"styles"`
	Genres            []DirectorOption     `json:"genres"`
	Moods             []string             `json:"moods"`
	Models            DirectorModels       `json:"models"`
	Limits            DirectorLimits       `json:"limits"`
	Cameras           []string             `json:"cameras"`
}
