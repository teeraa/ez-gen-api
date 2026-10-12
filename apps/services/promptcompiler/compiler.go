// Package promptcompiler converts structured creative inputs into a provider-ready
// generation request. It is deliberately free of network calls and database
// access so it can be tested deterministically without external dependencies.
package promptcompiler

import (
	"errors"
	"strings"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

// Result is returned by Compile and contains everything needed to
// (a) submit a generation job and (b) store an immutable effective-inputs snapshot.
type Result struct {
	// CompiledPrompt is the final text prompt sent to the provider.
	CompiledPrompt string
	// NegativeInstructions holds tokens from negative_constraint tags (appended separately).
	NegativeInstructions string
	// ContentItems is the ordered list ready for the ModelArk CreateTaskRequest.
	ContentItems []modelark.ContentItem
	// ProviderModelID is the BytePlus API model identifier.
	ProviderModelID string
	// Resolution is the provider resolution string derived from the quality field.
	Resolution string
	// GenerateAudio indicates whether the model supports and should generate audio.
	GenerateAudio bool
	// Snapshot is a serialisable copy of the resolved inputs for storage.
	Snapshot EffectiveInputsSnapshot
}

// EffectiveInputsSnapshot is the immutable record stored in
// video_generation_jobs.effective_inputs (JSONB). Editing profiles after
// submission does not change this record.
type EffectiveInputsSnapshot struct {
	CharacterProfileID      string          `json:"characterProfileId,omitempty"`
	CharacterProfileVersion int             `json:"characterProfileVersion,omitempty"`
	StyleProfileID          string          `json:"styleProfileId,omitempty"`
	SceneID                 string          `json:"sceneId,omitempty"`
	Tags                    []SnapshotTag   `json:"tags,omitempty"`
	Assets                  []SnapshotAsset `json:"assets,omitempty"`
	ModelID                 string          `json:"modelId"`
	Resolution              string          `json:"resolution"`
	CharacterSummary        string          `json:"characterSummary,omitempty"`
	StyleSummary            string          `json:"styleSummary,omitempty"`
	SceneSummary            string          `json:"sceneSummary,omitempty"`
}

type SnapshotTag struct {
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Category string `json:"category"`
}

type SnapshotAsset struct {
	UploadID string `json:"uploadId"`
	Role     string `json:"role"`
}

// Compile is deterministic: same inputs always produce the same output.
// It does not make network calls or read from the database.
func Compile(in Inputs, model modelark.ModelConfig) (*Result, error) {

	// ── 1. Validate asset support ──────────────────────────────────────────────
	mode, err := resolveMode(in.Assets)
	if err != nil {
		return nil, err
	}
	if mode == "image_video" && !model.SupportsImgVideo {
		return nil, errors.New("selected model does not support image-to-video")
	}

	// ── 2. Compile text prompt sections ──────────────────────────────────────
	var sections []string

	// 2a. Scene description / action (most specific — goes first)
	if in.Scene != nil {
		if in.Scene.ActionOverride != "" {
			sections = append(sections, in.Scene.ActionOverride)
		} else if in.Scene.Description != "" {
			sections = append(sections, in.Scene.Description)
		}
	}

	// 2b. Character appearance (if no scene override redefines the subject)
	if in.Character != nil {
		charDesc := buildCharacterDescription(in.Character)
		if charDesc != "" {
			sections = append(sections, charDesc)
		}
	}

	// 2c. Style attributes — camera uses scene override when present
	if in.Style != nil {
		styleDesc := buildStyleDescription(in.Style, in.Scene)
		if styleDesc != "" {
			sections = append(sections, styleDesc)
		}
	}

	// 2d. Non-negative tags contribute prompt tokens
	var positiveTagTokens []string
	var negativeTagTokens []string
	for _, tag := range in.Tags {
		if tag.Category == models.TagCategoryNegativeConstraint {
			negativeTagTokens = append(negativeTagTokens, tag.Name)
		} else {
			positiveTagTokens = append(positiveTagTokens, tag.Name)
		}
	}
	if len(positiveTagTokens) > 0 {
		sections = append(sections, strings.Join(positiveTagTokens, ", "))
	}

	// 2e. Consistency notes appended at the end of the character section
	if in.Character != nil && in.Character.ConsistencyNotes != "" {
		sections = append(sections, in.Character.ConsistencyNotes)
	}

	compiledPrompt := strings.Join(sections, ". ")
	if len(compiledPrompt) > 2000 {
		compiledPrompt = compiledPrompt[:2000]
	}

	negativeInstructions := strings.Join(negativeTagTokens, ", ")

	// ── 3. Build content items (text first, then images in role order) ─────────
	items := []modelark.ContentItem{
		{Type: modelark.ContentTypeText, Text: compiledPrompt},
	}
	for _, asset := range in.Assets {
		role := toContentRole(asset.Role)
		ct := modelark.ContentTypeImageURL
		if strings.HasPrefix(asset.MimeType, "video/") {
			ct = modelark.ContentTypeVideoURL
		}
		items = append(items, modelark.ContentItem{
			Type: ct,
			URL:  asset.StorageKey, // facade replaces with a signed URL before submission
			Role: role,
		})
	}

	resolution := modelark.QualityToResolution(in.Quality)

	// ── 4. Build snapshot ─────────────────────────────────────────────────────
	snap := EffectiveInputsSnapshot{
		ModelID:    in.ModelID,
		Resolution: resolution,
	}
	if in.Character != nil {
		snap.CharacterProfileID = in.Character.ProfileID
		snap.CharacterProfileVersion = in.Character.ProfileVersion
		snap.CharacterSummary = buildCharacterDescription(in.Character)
	}
	if in.Style != nil {
		snap.StyleProfileID = in.Style.ProfileID
		snap.StyleSummary = buildStyleDescription(in.Style, in.Scene)
	}
	if in.Scene != nil {
		snap.SceneID = in.Scene.SceneID
		snap.SceneSummary = in.Scene.Description
	}
	for _, tag := range in.Tags {
		snap.Tags = append(snap.Tags, SnapshotTag{
			ID:       tag.ID.String(),
			Slug:     tag.Slug,
			Category: string(tag.Category),
		})
	}
	for _, asset := range in.Assets {
		snap.Assets = append(snap.Assets, SnapshotAsset{
			UploadID: asset.UploadID,
			Role:     asset.Role,
		})
	}

	return &Result{
		CompiledPrompt:       compiledPrompt,
		NegativeInstructions: negativeInstructions,
		ContentItems:         items,
		ProviderModelID:      model.ProviderModelID,
		Resolution:           resolution,
		GenerateAudio:        model.SupportsAudio,
		Snapshot:             snap,
	}, nil
}

// ─── private helpers ─────────────────────────────────────────────────────────

func resolveMode(assets []AssetRef) (string, error) {
	for _, a := range assets {
		if a.Role == "first_frame" {
			return "image_video", nil
		}
	}
	return "text", nil
}

func buildCharacterDescription(c *CharacterInputs) string {
	var parts []string
	if c.Appearance != "" {
		parts = append(parts, c.Appearance)
	}
	if c.Hairstyle != "" {
		parts = append(parts, c.Hairstyle+" hair")
	}
	if c.Clothing != "" {
		parts = append(parts, "wearing "+c.Clothing)
	}
	if c.Accessories != "" {
		parts = append(parts, c.Accessories)
	}
	if c.DefiningFeatures != "" {
		parts = append(parts, c.DefiningFeatures)
	}
	return strings.Join(parts, ", ")
}

func buildStyleDescription(s *StyleInputs, scene *SceneInputs) string {
	var parts []string
	if s.VisualStyle != "" {
		parts = append(parts, s.VisualStyle)
	}
	if s.ColorPalette != "" {
		parts = append(parts, s.ColorPalette+" color palette")
	}

	// Camera: scene override takes precedence over style profile
	camera := s.CameraLanguage
	if scene != nil && scene.CameraOverride != "" {
		camera = scene.CameraOverride
	}
	if camera != "" {
		parts = append(parts, camera)
	}

	// Setting: scene override takes precedence
	setting := ""
	if scene != nil && scene.SettingOverride != "" {
		setting = scene.SettingOverride
	}
	if setting != "" {
		parts = append(parts, setting)
	}

	// Lighting comes from style unless scene overrides setting entirely
	if s.Lighting != "" {
		parts = append(parts, s.Lighting+" lighting")
	}

	mood := s.Mood
	if mood != "" {
		parts = append(parts, mood+" mood")
	}
	if s.QualityNotes != "" {
		parts = append(parts, s.QualityNotes)
	}
	return strings.Join(parts, ", ")
}

func toContentRole(role string) modelark.ContentRole {
	switch role {
	case "first_frame":
		return modelark.RoleFirstFrame
	case "last_frame":
		return modelark.RoleLastFrame
	case "reference":
		return modelark.RoleReferenceImage
	default:
		return modelark.RoleReferenceImage
	}
}
