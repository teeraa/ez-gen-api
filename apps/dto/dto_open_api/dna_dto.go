package dto_open_api

// TagDTO represents a single DNA tag.
type TagDTO struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	SortOrder   int    `json:"sortOrder"`
	CreatedAt   string `json:"createdAt"`
}

// CharacterProfileDTO is the API response shape for a character profile.
type CharacterProfileDTO struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Appearance       string   `json:"appearance"`
	Hairstyle        string   `json:"hairstyle"`
	Clothing         string   `json:"clothing"`
	Accessories      string   `json:"accessories"`
	DefiningFeatures string   `json:"definingFeatures"`
	ConsistencyNotes string   `json:"consistencyNotes"`
	Version          int      `json:"version"`
	Tags             []TagDTO `json:"tags"`
	CreatedAt        string   `json:"createdAt"`
	UpdatedAt        string   `json:"updatedAt"`
}

// CharacterProfileListDTO wraps a paginated character profile list.
type CharacterProfileListDTO struct {
	Items []CharacterProfileDTO `json:"items"`
	Total int                   `json:"total"`
	Page  int                   `json:"page"`
	Limit int                   `json:"limit"`
}

// StyleProfileDTO is the API response shape for a style profile.
type StyleProfileDTO struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	VisualStyle    string   `json:"visualStyle"`
	ColorPalette   string   `json:"colorPalette"`
	Lighting       string   `json:"lighting"`
	Mood           string   `json:"mood"`
	CameraLanguage string   `json:"cameraLanguage"`
	QualityNotes   string   `json:"qualityNotes"`
	Tags           []TagDTO `json:"tags"`
	CreatedAt      string   `json:"createdAt"`
	UpdatedAt      string   `json:"updatedAt"`
}

// StyleProfileListDTO wraps a paginated style profile list.
type StyleProfileListDTO struct {
	Items []StyleProfileDTO `json:"items"`
	Total int               `json:"total"`
	Page  int               `json:"page"`
	Limit int               `json:"limit"`
}

// SceneDTO is the API response shape for a scene.
type SceneDTO struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// CharacterProfileID is the first entry of Characters, kept for older clients.
	CharacterProfileID *string             `json:"characterProfileId"`
	Characters         []SceneCharacterDTO `json:"characters"`
	StyleProfileID     *string             `json:"styleProfileId"`
	PreviousSceneID    *string             `json:"previousSceneId"`
	SelectedJobID      *string             `json:"selectedJobId"`
	SceneOrder         int                 `json:"sceneOrder"`
	Tags               []TagDTO            `json:"tags"`
	CreatedAt          string              `json:"createdAt"`
	UpdatedAt          string              `json:"updatedAt"`
	// Preview is null when the scene has no succeeded output yet.
	Preview *ScenePreviewDTO `json:"preview"`
}

// ScenePreviewDTO shows a scene's output in the storyboard: the selected job, or the
// latest succeeded job when none is selected. URLs are signed and expire at ExpiresAt.
type ScenePreviewDTO struct {
	JobID        string  `json:"jobId"`
	IsSelected   bool    `json:"isSelected"`
	ThumbnailURL *string `json:"thumbnailUrl"`
	MediaURL     string  `json:"mediaUrl"`
	MediaType    string  `json:"mediaType"`
	ExpiresAt    *string `json:"expiresAt"`
}

type SceneCharacterDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
}

// SceneListDTO wraps a paginated scene list.
type SceneListDTO struct {
	Items []SceneDTO `json:"items"`
	Total int        `json:"total"`
	Page  int        `json:"page"`
	Limit int        `json:"limit"`
}
