// Package promptcompiler converts structured creative inputs into a provider-ready
// generation request. It is deliberately free of network calls and database
// access so it can be tested deterministically without external dependencies.
package promptcompiler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/characterdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

var ErrPromptBudget = errors.New("dialogue exceeds prompt budget")

// Result is returned by Compile and contains everything needed to
// (a) submit a generation job and (b) store an immutable effective-inputs snapshot.
type Result struct {
	// CompiledPrompt is the final text prompt sent to the provider.
	CompiledPrompt string
	// NegativeInstructions mirrors negative tags also included in the provider text.
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
	PipelineRenderingVersion string `json:"pipelineRenderingVersion,omitempty"`
	Dialogue                 string `json:"dialogue,omitempty"`
	// CharacterProfileID/Version mirror Characters[0] for rows written before
	// multi-character scenes.
	CharacterProfileID      string              `json:"characterProfileId,omitempty"`
	CharacterProfileVersion int                 `json:"characterProfileVersion,omitempty"`
	Characters              []SnapshotCharacter `json:"characters,omitempty"`
	StyleProfileID          string              `json:"styleProfileId,omitempty"`
	SceneID                 string              `json:"sceneId,omitempty"`
	Tags                    []SnapshotTag       `json:"tags,omitempty"`
	Assets                  []SnapshotAsset     `json:"assets,omitempty"`
	ModelID                 string              `json:"modelId"`
	Resolution              string              `json:"resolution"`
	CharacterSummary        string              `json:"characterSummary,omitempty"`
	StyleSummary            string              `json:"styleSummary,omitempty"`
	SceneSummary            string              `json:"sceneSummary,omitempty"`
}

type SnapshotCharacter struct {
	ProfileID string `json:"profileId"`
	Version   int    `json:"version"`
	Name      string `json:"name"`
	Summary   string `json:"summary"`
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

	if utf8.RuneCountInString(in.Dialogue) > 1000 || (in.Dialogue != "" && !model.SupportsAudio) {
		return nil, errors.New("dialogue is not supported")
	}
	// ── 1. Validate asset support ──────────────────────────────────────────────
	mode, err := resolveMode(in.Assets)
	if err != nil {
		return nil, err
	}
	if mode == "image_video" && !model.SupportsImgVideo {
		return nil, errors.New("selected model does not support image-to-video")
	}

	// ── 2. Compile text prompt sections ──────────────────────────────────────
	controls, sections, negativeInstructions := assemblePrompt(in)

	// Protect normal-sized role, environment and negative controls from a long creative brief.
	// The overall rune budget still applies when control text alone exceeds it.
	compiledPrompt := truncateRunes(strings.Join(append(controls, sections...), ". "), maxPromptRunes)
	if in.Dialogue != "" {
		speech := spokenDialogue(in.Dialogue, model.ProviderModelID)
		budget := maxPromptRunes - utf8.RuneCountInString(speech) - 2
		if budget < 0 || utf8.RuneCountInString(strings.Join(append(controls, sections...), ". ")) > budget {
			return nil, ErrPromptBudget
		}
		compiledPrompt = speech + ". " + truncateRunes(strings.Join(append(controls, sections...), ". "), budget)
	}

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
		PipelineRenderingVersion: in.PipelineRenderingVersion,
		Dialogue:                 in.Dialogue,
		ModelID:                  in.ModelID,
		Resolution:               resolution,
	}
	for i := range in.Characters {
		c := &in.Characters[i]
		snap.Characters = append(snap.Characters, SnapshotCharacter{
			ProfileID: c.ProfileID,
			Version:   c.ProfileVersion,
			Name:      c.Name,
			Summary:   buildCharacterDescription(c),
		})
	}
	if len(in.Characters) > 0 {
		first := &in.Characters[0]
		snap.CharacterProfileID = first.ProfileID
		snap.CharacterProfileVersion = first.ProfileVersion
		snap.CharacterSummary = buildCharacterDescription(first)
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
		GenerateAudio:        model.SupportsAudio && (in.Dialogue != "" || (model.ProviderModelID != "dreamina-seedance-2-0-fast-260128" && model.ProviderModelID != "dreamina-seedance-2-0-260128")),
		Snapshot:             snap,
	}, nil
}

// ─── private helpers ─────────────────────────────────────────────────────────

// assemblePrompt builds the control and content sections of the text prompt. Compile and
// PromptUsage both use it, so a preflight budget always matches what Compile enforces.
func assemblePrompt(in Inputs) (controls, sections []string, negativeInstructions string) {
	controls = referenceControls(in.Assets, in.PipelineRenderingVersion)
	if in.Scene != nil {
		if in.Scene.SettingOverride != "" {
			controls = append(controls, "Scene environment: "+in.Scene.SettingOverride)
		}
		if in.Scene.CameraOverride != "" {
			controls = append(controls, "Scene camera: "+in.Scene.CameraOverride)
		}
	}

	// 2a. Scene description / action (most specific — goes first)
	if in.Scene != nil {
		if in.Scene.ActionOverride != "" {
			sections = append(sections, in.Scene.ActionOverride)
		} else if in.Scene.Description != "" {
			sections = append(sections, in.Scene.Description)
		}
	}

	// 2b. Character appearance. Each description is labelled with the character's
	// name so the model can tell who is who, and capped so one verbose profile
	// cannot crowd the others out of the prompt limit.
	characterBudget := maxCharacterSectionRunes
	if n := len(in.Characters); n > 1 {
		characterBudget = maxCharacterSectionRunes / n
	}
	for i := range in.Characters {
		if desc := labelledCharacter(&in.Characters[i], buildCharacterDescription(&in.Characters[i])); desc != "" {
			sections = append(sections, truncateRunes(desc, characterBudget))
		}
	}

	// 2c. Style attributes — camera uses scene override when present
	if in.Style != nil {
		style := *in.Style
		if in.Scene != nil && in.Scene.CameraOverride != "" {
			style.CameraLanguage = ""
		}
		styleDesc := buildStyleDescription(&style, nil)
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
	for i := range in.Characters {
		if notes := in.Characters[i].ConsistencyNotes; notes != "" {
			sections = append(sections, truncateRunes(labelledCharacter(&in.Characters[i], notes), characterBudget))
		}
	}

	negativeInstructions = strings.Join(negativeTagTokens, ", ")
	if negativeInstructions != "" {
		controls = append(controls, "Constraints: "+negativeInstructions)
	}
	return controls, sections, negativeInstructions
}

// PromptUsage reports the runes the compiled prompt needs (controls, scene, characters and
// the spoken-dialogue instruction) against the provider limit. Without dialogue Compile
// truncates an overlong prompt; with dialogue it rejects it with ErrPromptBudget.
func PromptUsage(in Inputs, model modelark.ModelConfig) (used, limit int) {
	controls, sections, _ := assemblePrompt(in)
	used = utf8.RuneCountInString(strings.Join(append(controls, sections...), ". "))
	if in.Dialogue != "" {
		used += utf8.RuneCountInString(spokenDialogue(in.Dialogue, model.ProviderModelID)) + 2
	}
	return used, maxPromptRunes
}

func spokenDialogue(dialogue, providerModelID string) string {
	quoted, _ := json.Marshal(dialogue)
	if providerModelID == "dreamina-seedance-2-5-260628" && predominantlyThaiDialogue(dialogue) {
		// Seedance 2.5 distinguishes dialogue with braces and a language prefix.
		return "Spoken dialogue, say these exact words with natural lip sync and native audio, not subtitles; clear native Thai pronunciation, natural unhurried pace: {Thai: " + string(quoted) + "}"
	}
	return "Spoken dialogue, say these exact words with natural lip sync and native audio, not subtitles: " + string(quoted)
}

// A language hint is conservative: brands or short Thai fragments inside another
// language must not relabel the whole utterance. Marks and repetition signs do not
// contribute to the majority, and every spoken word remains untouched.
func predominantlyThaiDialogue(text string) bool {
	thai, other := 0, 0
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		if unicode.Is(unicode.Thai, r) {
			if r != 'ฯ' && r != 'ๆ' {
				thai++
			}
		} else {
			other++
		}
	}
	return thai > other
}

func referenceControls(assets []AssetRef, pipelineRenderingVersion string) []string {
	var controls []string
	imageIndex := 0
	hasImageSubject := false
	for _, asset := range assets {
		if strings.HasPrefix(asset.MimeType, "video/") {
			continue
		}
		imageIndex++
		switch toContentRole(asset.Role) {
		case modelark.RoleFirstFrame:
			hasImageSubject = true
			controls = append(controls, fmt.Sprintf("Image %d: preserve the complete supplied starting frame, including its composition and background, as the video start", imageIndex))
		case modelark.RoleLastFrame:
			controls = append(controls, fmt.Sprintf("Image %d: preserve the complete supplied ending frame, including its composition and background, as the video end", imageIndex))
		case modelark.RoleReferenceImage:
			hasImageSubject = true
			if pipelineRenderingVersion == PipelineFilmedHumanVideoVersion {
				controls = append(controls, fmt.Sprintf("Image %d: casting/design guide for face/body geometry, haircut and outfit only; no beautification or identity drift. Exclude its shading, CG materials, drawn skin, pose and background. Not a texture/style/start frame; follow scene setting, action and camera.", imageIndex))
			} else {
				controls = append(controls, fmt.Sprintf("Image %d: use only the subject appearance as a visual reference; exclude its source background. Do not infer the scene environment from this reference. Use the existing scene description and any explicit scene environment override; when neither specifies an environment, no specific replacement environment is prescribed", imageIndex))
			}
		}
	}
	if hasImageSubject && pipelineRenderingVersion == PipelineFilmedHumanVideoVersion {
		controls = append(controls, "Filmed live-action human actor look: anatomically plausible skin with subtle texture and translucency, individual hair and real cloth. Natural eyes, blinks, expressions, spoken lip sync and grounded motion. Motivated practical lighting, natural shadows, optical depth and subtle shutter motion blur. Preserve designed identity as a filmed person, not CG/cartoon/plastic/doll; follow scene framing.")
	} else if hasImageSubject && pipelineRenderingVersion == PipelineCinematicVideoVersion {
		controls = append(controls, "Reference face/body/hair/clothes identity only, not style. Photoreal live-action film humans: skin microtexture/imperfections, hair/fabric; grounded weight/motion, expressions, blinks, natural lip sync. Turn matte CG to human materials. Motivated lighting, natural shadows, optical depth, subtle motion blur. No CG/cartoon/plastic/doll; follow scene camera/framing.")
	} else if hasImageSubject && (pipelineRenderingVersion == characterdesign.PipelineRealisticCompilerVersion || pipelineRenderingVersion == characterdesign.PipelineSoftRealisticCompilerVersion || pipelineRenderingVersion == characterdesign.PipelineSemiRealisticCompilerVersion || pipelineRenderingVersion == characterdesign.PipelinePolished3DCompilerVersion || pipelineRenderingVersion == characterdesign.PipelineStylized3DCompilerVersion) {
		controls = append(controls, "Render the fictional character as a photorealistic live-action human: realistic skin texture, hair, fabric, natural motion and cinematic lighting. Preserve similar facial structure, body proportions, hairstyle and clothing with stable identity and coherent anatomy. Replace digital reference rendering with live-action realism; avoid cartoon or plastic CG appearance.")
	} else if hasImageSubject && pipelineRenderingVersion == characterdesign.PipelineCompilerVersion {
		controls = append(controls, "Render the fictional character with realistic cinematic CG lighting, material response and natural motion, coherent anatomy and stable facial identity. Preserve the designed face, proportions, hair and clothing; add believable physical detail without replacing the character with a real person. Retain explicit anime, illustration or clay styling while making motion and lighting physically convincing")
	} else if hasImageSubject {
		controls = append(controls, "Maintain referenced character facial structure, proportions, hair and clothing across frames unless explicitly changed. Animate with natural believable motion, coherent anatomy, physically plausible lighting and material response, and temporal stability without facial drift or flicker. Preserve the selected visual style, including stylized animation; do not reinterpret it as live action")
	}
	return controls
}

func resolveMode(assets []AssetRef) (string, error) {
	for _, a := range assets {
		if a.Role == "first_frame" {
			return "image_video", nil
		}
	}
	return "text", nil
}

const (
	maxPromptRunes           = 2000
	maxCharacterSectionRunes = 750
)

func labelledCharacter(c *CharacterInputs, text string) string {
	if text == "" {
		return ""
	}
	if c.Name == "" {
		return text
	}
	return c.Name + ": " + text
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
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
