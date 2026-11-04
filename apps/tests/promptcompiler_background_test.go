package tests

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
)

func TestCompileReferenceBackgroundRoleAndImageOrder(t *testing.T) {
	t.Parallel()
	input := promptcompiler.Inputs{ModelID: "violet-motion-2", Quality: "standard", Scene: &promptcompiler.SceneInputs{Description: "The subject walks through a night market", SettingOverride: "night market", CameraOverride: "slow dolly"}, Characters: []promptcompiler.CharacterInputs{{Name: "Hero", Appearance: "blue jacket", ConsistencyNotes: "keep the blue jacket"}}, Assets: []promptcompiler.AssetRef{
		{StorageKey: "video-fixture", Role: "reference", MimeType: "video/mp4"},
		{StorageKey: "start-fixture", Role: "first_frame", MimeType: "image/png"},
		{StorageKey: "subject-fixture", Role: "reference", MimeType: "image/png"},
		{StorageKey: "end-fixture", Role: "last_frame", MimeType: "image/jpeg"},
	}}
	result, err := compileForTest(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Image 2: use only the subject appearance", "exclude its source background", "night market", "slow dolly", "Image 1: preserve the complete supplied starting frame", "Image 3: preserve the complete supplied ending frame", "keep the blue jacket"} {
		if !strings.Contains(result.CompiledPrompt, want) {
			t.Errorf("missing %q in %q", want, result.CompiledPrompt)
		}
	}
	if strings.Contains(result.CompiledPrompt, "Image 1: use only") || strings.Contains(result.CompiledPrompt, "Image 3: use only") {
		t.Fatal("subject-only controls applied to frame inputs")
	}
	if len(result.ContentItems) != 5 || result.ContentItems[1].Type != modelark.ContentTypeVideoURL || result.ContentItems[2].Role != modelark.RoleFirstFrame || result.ContentItems[3].Role != modelark.RoleReferenceImage || result.ContentItems[4].Role != modelark.RoleLastFrame {
		t.Fatalf("media order or roles changed: %+v", result.ContentItems)
	}
	for i, asset := range input.Assets {
		if result.ContentItems[i+1].URL != asset.StorageKey {
			t.Fatal("media order changed")
		}
	}
	if result.ContentItems[0].Text != result.CompiledPrompt {
		t.Fatal("controls not in provider text")
	}
}
func TestCompileBackgroundControlsSurviveLongBrief(t *testing.T) {
	t.Parallel()
	input := promptcompiler.Inputs{ModelID: "violet-motion-2", Quality: "standard", Scene: &promptcompiler.SceneInputs{Description: strings.Repeat("เดินผ่านตลาด ", 400), SettingOverride: "ตลาดกลางคืน", CameraOverride: "tracking camera"}, Assets: []promptcompiler.AssetRef{{Role: "reference", MimeType: "image/png"}}, Tags: []models.DNATag{{Name: "No source backdrop", Category: models.TagCategoryNegativeConstraint}, {Name: "No extra signage", Category: models.TagCategoryNegativeConstraint}}}
	result, err := compileForTest(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Image 1: use only the subject appearance", "ตลาดกลางคืน", "tracking camera", "Constraints: No source backdrop, No extra signage"} {
		if !strings.Contains(result.ContentItems[0].Text, want) {
			t.Errorf("control truncated: %q", want)
		}
	}
	if !utf8.ValidString(result.CompiledPrompt) || utf8.RuneCountInString(result.CompiledPrompt) > 2000 {
		t.Fatal("prompt rune limit changed")
	}
}
func TestCompileReferenceWithoutEnvironmentPrescribesNoReplacement(t *testing.T) {
	t.Parallel()
	result, err := compileForTest(promptcompiler.Inputs{ModelID: "violet-motion-2", Quality: "standard", Assets: []promptcompiler.AssetRef{{Role: "reference", MimeType: "image/png"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.CompiledPrompt, "no specific replacement environment is prescribed") {
		t.Fatalf("missing unspecified-environment guidance: %s", result.CompiledPrompt)
	}
	for _, invented := range []string{"white background", "neutral background", "studio background"} {
		if strings.Contains(result.CompiledPrompt, invented) {
			t.Fatalf("invented environment %s", invented)
		}
	}
}
func TestCompileFirstFrameKeepsCompositionWithoutSubjectOnlyControl(t *testing.T) {
	t.Parallel()
	result, err := compileForTest(promptcompiler.Inputs{ModelID: "violet-motion-2", Quality: "standard", Scene: &promptcompiler.SceneInputs{Description: "A boat drifts"}, Assets: []promptcompiler.AssetRef{{Role: "first_frame", MimeType: "image/png"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.CompiledPrompt, "preserve the complete supplied starting frame") || strings.Contains(result.CompiledPrompt, "exclude its source background") || strings.Contains(result.CompiledPrompt, "use only the subject") {
		t.Fatalf("first frame semantics changed: %s", result.CompiledPrompt)
	}
}
