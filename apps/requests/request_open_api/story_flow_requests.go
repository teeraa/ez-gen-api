package request_open_api

type CreateStoryFlowRequest struct {
	Title              string   `json:"title"`
	Idea               string   `json:"idea"`
	Style              string   `json:"style"`
	ModelID            string   `json:"modelId"`
	Quality            string   `json:"quality"`
	AspectRatio        string   `json:"aspectRatio"`
	SceneCount         int      `json:"sceneCount"`
	MoodTags           []string `json:"moodTags"`
	CharacterAssetIDs  []string `json:"characterAssetIds"`
	NewCharacterPrompt string   `json:"newCharacterPrompt"`
	IdempotencyKey     string   `json:"idempotencyKey"`
}
type StoryFlowSceneShots struct {
	SceneIndex int `json:"sceneIndex"`
	ShotCount  int `json:"shotCount"`
}
type StoryFlowActionRequest struct {
	Revision       int                   `json:"revision"`
	IdempotencyKey string                `json:"idempotencyKey"`
	SceneShots     []StoryFlowSceneShots `json:"sceneShots,omitempty"`
	Stage          string                `json:"stage,omitempty"`
	ShotID         string                `json:"shotId,omitempty"`
}
