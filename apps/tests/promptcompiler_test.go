package tests

import (
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
)

func TestCompile_TextOnly(t *testing.T) {
	t.Parallel()
	in := promptcompiler.Inputs{
		ModelID: "violet-motion-2",
		Quality: "standard",
		Scene: &promptcompiler.SceneInputs{
			Description: "A warrior stands on a hill at dusk",
		},
	}
	result, err := promptcompiler.Compile(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CompiledPrompt == "" {
		t.Error("expected non-empty compiled prompt")
	}
	if !strings.Contains(result.CompiledPrompt, "warrior") {
		t.Errorf("expected scene description in prompt, got: %q", result.CompiledPrompt)
	}
	if result.ProviderModelID != "dreamina-seedance-2-0-fast-260128" {
		t.Errorf("unexpected provider model ID: %q", result.ProviderModelID)
	}
	if result.Resolution != "720p" {
		t.Errorf("expected 720p for standard quality, got: %q", result.Resolution)
	}
}

func TestCompile_WithCharacterAndStyle(t *testing.T) {
	t.Parallel()
	in := promptcompiler.Inputs{
		ModelID: "violet-realism",
		Quality: "high",
		Character: &promptcompiler.CharacterInputs{
			ProfileID:  "char-1",
			Name:       "Elena",
			Appearance: "tall woman with sharp features",
			Clothing:   "crimson armour",
		},
		Style: &promptcompiler.StyleInputs{
			ProfileID:      "style-1",
			VisualStyle:    "cinematic",
			CameraLanguage: "slow dolly",
			Lighting:       "golden hour",
			Mood:           "epic",
		},
		Scene: &promptcompiler.SceneInputs{
			SceneID:     "scene-1",
			Description: "Elena charges toward the gate",
		},
	}
	result, err := promptcompiler.Compile(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result.CompiledPrompt, "Elena charges") {
		t.Errorf("expected scene description in prompt, got: %q", result.CompiledPrompt)
	}
	if !strings.Contains(result.CompiledPrompt, "crimson armour") {
		t.Errorf("expected character clothing in prompt, got: %q", result.CompiledPrompt)
	}
	if !strings.Contains(result.CompiledPrompt, "cinematic") {
		t.Errorf("expected visual style in prompt, got: %q", result.CompiledPrompt)
	}
	if result.Resolution != "1080p" {
		t.Errorf("expected 1080p for high quality, got: %q", result.Resolution)
	}
	if result.GenerateAudio != true {
		t.Error("expected violet-realism to generate audio")
	}

	// Snapshot must record character profile ID and version
	if result.Snapshot.CharacterProfileID != "char-1" {
		t.Errorf("snapshot character profile ID mismatch")
	}
}

func TestCompile_CameraOverrideFromScene(t *testing.T) {
	t.Parallel()
	in := promptcompiler.Inputs{
		ModelID: "violet-motion-2",
		Quality: "standard",
		Style: &promptcompiler.StyleInputs{
			ProfileID:      "style-1",
			CameraLanguage: "static wide",
		},
		Scene: &promptcompiler.SceneInputs{
			CameraOverride: "extreme close-up",
		},
	}
	result, err := promptcompiler.Compile(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.CompiledPrompt, "extreme close-up") {
		t.Errorf("scene camera override should replace style camera, got: %q", result.CompiledPrompt)
	}
	if strings.Contains(result.CompiledPrompt, "static wide") {
		t.Errorf("style camera should be overridden by scene, got: %q", result.CompiledPrompt)
	}
}

func TestCompile_NegativeTagsIsolated(t *testing.T) {
	t.Parallel()
	in := promptcompiler.Inputs{
		ModelID: "violet-motion-2",
		Quality: "standard",
		Scene: &promptcompiler.SceneInputs{Description: "forest at night"},
		Tags: []models.DNATag{
			{Slug: "avoid_text", Name: "Avoid Text", Category: models.TagCategoryNegativeConstraint},
			{Slug: "cinematic", Name: "Cinematic", Category: models.TagCategoryVisualStyle},
		},
	}
	result, err := promptcompiler.Compile(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result.CompiledPrompt, "Avoid Text") {
		t.Error("negative constraint tag must not appear in compiled prompt")
	}
	if result.NegativeInstructions != "Avoid Text" {
		t.Errorf("expected negative instructions = 'Avoid Text', got: %q", result.NegativeInstructions)
	}
	if !strings.Contains(result.CompiledPrompt, "Cinematic") {
		t.Errorf("positive tag should appear in compiled prompt, got: %q", result.CompiledPrompt)
	}
}

func TestCompile_UnknownModelError(t *testing.T) {
	t.Parallel()
	in := promptcompiler.Inputs{
		ModelID: "nonexistent-model",
		Quality: "standard",
		Scene:   &promptcompiler.SceneInputs{Description: "test"},
	}
	_, err := promptcompiler.Compile(in)
	if err == nil {
		t.Error("expected error for unknown model, got nil")
	}
}

func TestCompile_PromptTruncatedAt2000(t *testing.T) {
	t.Parallel()
	longText := strings.Repeat("A ", 1200)
	in := promptcompiler.Inputs{
		ModelID: "violet-motion-2",
		Quality: "standard",
		Scene:   &promptcompiler.SceneInputs{Description: longText},
	}
	result, err := promptcompiler.Compile(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.CompiledPrompt) > 2000 {
		t.Errorf("compiled prompt exceeds 2000 chars: %d", len(result.CompiledPrompt))
	}
}

func TestCompile_ImageToVideoRequiresFirstFrameAsset(t *testing.T) {
	t.Parallel()
	in := promptcompiler.Inputs{
		ModelID: "violet-motion-2",
		Quality: "standard",
		Scene:   &promptcompiler.SceneInputs{Description: "zoom out"},
		Assets: []promptcompiler.AssetRef{
			{UploadID: "upload-1", StorageKey: "s3://bucket/img.jpg", Role: "first_frame", MimeType: "image/jpeg"},
		},
	}
	result, err := promptcompiler.Compile(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ContentItems) < 2 {
		t.Error("expected text item + image item in content")
	}
	// First item must be text
	if result.ContentItems[0].Type != "text" {
		t.Errorf("first content item must be text, got: %s", result.ContentItems[0].Type)
	}
}

func TestCompile_SnapshotCaptures_ProfileVersions(t *testing.T) {
	t.Parallel()
	in := promptcompiler.Inputs{
		ModelID: "violet-motion-2",
		Quality: "standard",
		Scene:   &promptcompiler.SceneInputs{SceneID: "sc-1"},
		Character: &promptcompiler.CharacterInputs{
			ProfileID:      "cp-1",
			ProfileVersion: 3,
			Appearance:     "silver hair",
		},
		Style: &promptcompiler.StyleInputs{
			ProfileID:   "sp-1",
			VisualStyle: "anime",
		},
	}
	result, err := promptcompiler.Compile(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Snapshot.CharacterProfileVersion != 3 {
		t.Errorf("expected snapshot to record character profile version 3, got %d", result.Snapshot.CharacterProfileVersion)
	}
	if result.Snapshot.StyleProfileID != "sp-1" {
		t.Errorf("expected snapshot style profile ID, got %q", result.Snapshot.StyleProfileID)
	}
}

func TestCompile_SeedanceOnePro(t *testing.T) {
	t.Parallel()
	in := promptcompiler.Inputs{
		ModelID: "violet-classic",
		Quality: "high",
		Scene:   &promptcompiler.SceneInputs{Description: "A lantern drifts over a river"},
	}
	result, err := promptcompiler.Compile(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProviderModelID != "seedance-1-0-pro-250528" {
		t.Errorf("unexpected provider model ID: %q", result.ProviderModelID)
	}
	if result.GenerateAudio {
		t.Error("seedance 1.0 pro does not support audio; GenerateAudio must be false")
	}
	if result.Resolution != "1080p" {
		t.Errorf("expected 1080p for high quality, got: %q", result.Resolution)
	}
}
