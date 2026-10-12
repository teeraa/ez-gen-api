package tests

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
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
	result, err := compileForTest(in)
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
		Characters: []promptcompiler.CharacterInputs{{
			ProfileID:  "char-1",
			Name:       "Elena",
			Appearance: "tall woman with sharp features",
			Clothing:   "crimson armour",
		}},
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
	result, err := compileForTest(in)
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
	if !result.GenerateAudio {
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
	result, err := compileForTest(in)
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
		Scene:   &promptcompiler.SceneInputs{Description: "forest at night"},
		Tags: []models.DNATag{
			{Slug: "avoid_text", Name: "Avoid Text", Category: models.TagCategoryNegativeConstraint},
			{Slug: "cinematic", Name: "Cinematic", Category: models.TagCategoryVisualStyle},
		},
	}
	result, err := compileForTest(in)
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
	_, err := compileForTest(in)
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
	result, err := compileForTest(in)
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
	result, err := compileForTest(in)
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
		Characters: []promptcompiler.CharacterInputs{{
			ProfileID:      "cp-1",
			ProfileVersion: 3,
			Appearance:     "silver hair",
		}},
		Style: &promptcompiler.StyleInputs{
			ProfileID:   "sp-1",
			VisualStyle: "anime",
		},
	}
	result, err := compileForTest(in)
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
	result, err := compileForTest(in)
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

func compileForTest(in promptcompiler.Inputs) (*promptcompiler.Result, error) {
	registry, err := modelark.NewRegistry("")
	if err != nil {
		return nil, err
	}
	model, ok := registry.Lookup(in.ModelID)
	if !ok {
		return nil, fmt.Errorf("unknown model %q", in.ModelID)
	}
	return promptcompiler.Compile(in, model)
}

func TestCompile_MultipleCharactersAreLabelled(t *testing.T) {
	t.Parallel()
	in := promptcompiler.Inputs{
		ModelID: "violet-classic",
		Quality: "standard",
		Scene:   &promptcompiler.SceneInputs{Description: "Elena hands sticky rice to Somchai"},
		Characters: []promptcompiler.CharacterInputs{
			{ProfileID: "c1", ProfileVersion: 2, Name: "Elena", Appearance: "tall woman", Clothing: "crimson armour", ConsistencyNotes: "confident posture"},
			{ProfileID: "c2", ProfileVersion: 1, Name: "Somchai", Appearance: "elderly farmer", Accessories: "straw hat"},
		},
	}
	result, err := compileForTest(in)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, want := range []string{"Elena: tall woman, wearing crimson armour", "Somchai: elderly farmer, straw hat", "Elena: confident posture"} {
		if !strings.Contains(result.CompiledPrompt, want) {
			t.Errorf("prompt missing %q: %q", want, result.CompiledPrompt)
		}
	}
	if strings.Index(result.CompiledPrompt, "Elena:") > strings.Index(result.CompiledPrompt, "Somchai:") {
		t.Error("characters must keep their order")
	}
	if len(result.Snapshot.Characters) != 2 || result.Snapshot.Characters[1].ProfileID != "c2" || result.Snapshot.Characters[0].Version != 2 {
		t.Errorf("unexpected snapshot characters: %+v", result.Snapshot.Characters)
	}
	if result.Snapshot.CharacterProfileID != "c1" {
		t.Errorf("legacy snapshot field should mirror the first character, got %q", result.Snapshot.CharacterProfileID)
	}
}

func TestCompile_EachCharacterGetsPromptBudget(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("ผมยาวสีเงิน ", 200)
	in := promptcompiler.Inputs{
		ModelID: "violet-classic",
		Quality: "standard",
		Scene:   &promptcompiler.SceneInputs{Description: "two friends talk"},
		Characters: []promptcompiler.CharacterInputs{
			{ProfileID: "c1", Name: "Elena", Appearance: long},
			{ProfileID: "c2", Name: "Somchai", Appearance: "elderly farmer"},
		},
	}
	result, err := compileForTest(in)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(result.CompiledPrompt, "Somchai: elderly farmer") {
		t.Error("a verbose first character must not push the second out of the prompt")
	}
}

func TestCompile_TruncatesThaiByRune(t *testing.T) {
	t.Parallel()
	in := promptcompiler.Inputs{
		ModelID: "violet-classic",
		Quality: "standard",
		Scene:   &promptcompiler.SceneInputs{Description: strings.Repeat("จกข้าวเหนียว", 400)},
	}
	result, err := compileForTest(in)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !utf8.ValidString(result.CompiledPrompt) {
		t.Error("compiled prompt is not valid UTF-8")
	}
	if n := utf8.RuneCountInString(result.CompiledPrompt); n != 2000 {
		t.Errorf("prompt has %d runes, want 2000", n)
	}
}
