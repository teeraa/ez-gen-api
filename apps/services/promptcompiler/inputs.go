package promptcompiler

import "github.com/ez-gen/ez-gen-api/apps/models"

// Inputs holds all creative sources passed into the compiler.
// Every field is optional; the compiler degrades gracefully when absent.
type Inputs struct {
	// Character profile to draw appearance tokens from.
	Character *CharacterInputs
	// Style profile to draw visual/tone tokens from.
	Style *StyleInputs
	// Scene-specific instructions that override profile defaults.
	Scene *SceneInputs
	// Tags from ALL sources already resolved to their full structs.
	Tags []models.DNATag
	// Reference assets ordered by role priority (first_frame takes precedence).
	Assets []AssetRef
	// The model selected for generation — used to validate parameter support.
	ModelID string
	// Quality string ("standard" | "high") passed through to resolution mapping.
	Quality string
}

// CharacterInputs is a flattened view of a CharacterProfile at the moment of compilation.
// Populated from the live profile; stored in the effective-inputs snapshot.
type CharacterInputs struct {
	ProfileID        string
	ProfileVersion   int
	Name             string
	Appearance       string
	Hairstyle        string
	Clothing         string
	Accessories      string
	DefiningFeatures string
	ConsistencyNotes string
}

// StyleInputs is a flattened view of a StyleProfile.
type StyleInputs struct {
	ProfileID      string
	Name           string
	VisualStyle    string
	ColorPalette   string
	Lighting       string
	Mood           string
	CameraLanguage string
	QualityNotes   string
}

// SceneInputs holds scene-level instructions and per-scene overrides.
type SceneInputs struct {
	SceneID        string
	Description    string
	ActionOverride string // replaces inherited action
	CameraOverride string // overrides camera from style profile
	SettingOverride string // overrides environment from style profile
}

// AssetRef is a lightweight pointer to a reference asset.
type AssetRef struct {
	UploadID   string
	StorageKey string
	Role       string // 'reference' | 'first_frame' | 'last_frame' | 'keyframe'
	MimeType   string
}
