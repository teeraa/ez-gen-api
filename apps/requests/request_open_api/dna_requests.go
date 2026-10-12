package request_open_api

// ── Tags ───────────────────────────────────────────────────────────────────────

type ListTagsQuery struct {
	Category string `query:"category"`
}

// ── Character Profiles ─────────────────────────────────────────────────────────

type CreateCharacterProfileRequest struct {
	Name             string   `json:"name"             validate:"required,min=1,max=100"`
	Description      string   `json:"description"      validate:"max=1000"`
	Appearance       string   `json:"appearance"       validate:"max=500"`
	Hairstyle        string   `json:"hairstyle"        validate:"max=200"`
	Clothing         string   `json:"clothing"         validate:"max=300"`
	Accessories      string   `json:"accessories"      validate:"max=300"`
	DefiningFeatures string   `json:"definingFeatures" validate:"max=500"`
	ConsistencyNotes string   `json:"consistencyNotes" validate:"max=1000"`
	TagIDs           []string `json:"tagIds"           validate:"dive,uuid4"`
}

type UpdateCharacterProfileRequest struct {
	Name             *string  `json:"name"             validate:"omitempty,min=1,max=100"`
	Description      *string  `json:"description"      validate:"omitempty,max=1000"`
	Appearance       *string  `json:"appearance"       validate:"omitempty,max=500"`
	Hairstyle        *string  `json:"hairstyle"        validate:"omitempty,max=200"`
	Clothing         *string  `json:"clothing"         validate:"omitempty,max=300"`
	Accessories      *string  `json:"accessories"      validate:"omitempty,max=300"`
	DefiningFeatures *string  `json:"definingFeatures" validate:"omitempty,max=500"`
	ConsistencyNotes *string  `json:"consistencyNotes" validate:"omitempty,max=1000"`
	TagIDs           []string `json:"tagIds"           validate:"omitempty,dive,uuid4"`
}

// ── Style Profiles ─────────────────────────────────────────────────────────────

type CreateStyleProfileRequest struct {
	Name           string   `json:"name"           validate:"required,min=1,max=100"`
	Description    string   `json:"description"    validate:"max=1000"`
	VisualStyle    string   `json:"visualStyle"    validate:"max=200"`
	ColorPalette   string   `json:"colorPalette"   validate:"max=200"`
	Lighting       string   `json:"lighting"       validate:"max=200"`
	Mood           string   `json:"mood"           validate:"max=200"`
	CameraLanguage string   `json:"cameraLanguage" validate:"max=300"`
	QualityNotes   string   `json:"qualityNotes"   validate:"max=300"`
	TagIDs         []string `json:"tagIds"         validate:"dive,uuid4"`
}

type UpdateStyleProfileRequest struct {
	Name           *string  `json:"name"           validate:"omitempty,min=1,max=100"`
	Description    *string  `json:"description"    validate:"omitempty,max=1000"`
	VisualStyle    *string  `json:"visualStyle"    validate:"omitempty,max=200"`
	ColorPalette   *string  `json:"colorPalette"   validate:"omitempty,max=200"`
	Lighting       *string  `json:"lighting"       validate:"omitempty,max=200"`
	Mood           *string  `json:"mood"           validate:"omitempty,max=200"`
	CameraLanguage *string  `json:"cameraLanguage" validate:"omitempty,max=300"`
	QualityNotes   *string  `json:"qualityNotes"   validate:"omitempty,max=300"`
	TagIDs         []string `json:"tagIds"         validate:"omitempty,dive,uuid4"`
}

// ── Scenes ─────────────────────────────────────────────────────────────────────

type CreateSceneRequest struct {
	Title              string `json:"title"              validate:"max=200"`
	Description        string `json:"description"        validate:"max=2000"`
	CharacterProfileID string `json:"characterProfileId" validate:"omitempty,uuid4"`
	// CharacterProfileIDs (max 3, in display order) takes precedence over CharacterProfileID.
	CharacterProfileIDs []string `json:"characterProfileIds" validate:"omitempty,max=3,unique,dive,uuid4"`
	StyleProfileID      string   `json:"styleProfileId"     validate:"omitempty,uuid4"`
	PreviousSceneID     string   `json:"previousSceneId"    validate:"omitempty,uuid4"`
	SceneOrder          int      `json:"sceneOrder"`
	ActionOverride      string   `json:"actionOverride"     validate:"max=2000"`
	CameraOverride      string   `json:"cameraOverride"     validate:"max=200"`
	SettingOverride     string   `json:"settingOverride"    validate:"max=300"`
	TagIDs              []string `json:"tagIds"             validate:"dive,uuid4"`
}

type UpdateSceneRequest struct {
	Title              *string `json:"title"              validate:"omitempty,max=200"`
	Description        *string `json:"description"        validate:"omitempty,max=2000"`
	CharacterProfileID *string `json:"characterProfileId" validate:"omitempty,uuid4"`
	// CharacterProfileIDs replaces the scene's characters when present; [] clears them.
	CharacterProfileIDs []string `json:"characterProfileIds" validate:"omitempty,max=3,unique,dive,uuid4"`
	StyleProfileID      *string  `json:"styleProfileId"     validate:"omitempty,uuid4"`
	SceneOrder          *int     `json:"sceneOrder"`
	ActionOverride      *string  `json:"actionOverride"     validate:"omitempty,max=2000"`
	CameraOverride      *string  `json:"cameraOverride"     validate:"omitempty,max=200"`
	SettingOverride     *string  `json:"settingOverride"    validate:"omitempty,max=300"`
	TagIDs              []string `json:"tagIds"             validate:"omitempty,dive,uuid4"`
}
