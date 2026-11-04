package tests

import (
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
)

func TestImageConditionedVideoPreservesCharacterAndStyleWithNaturalMotion(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"first_frame", "reference"} {
		for _, style := range []string{"cinematic realistic", "anime", "clay"} {
			t.Run(role+"/"+style, func(t *testing.T) {
				in := promptcompiler.Inputs{ModelID: "violet-motion-2", Quality: "standard", Scene: &promptcompiler.SceneInputs{Description: "A synthetic character walks and turns toward the camera"}, Style: &promptcompiler.StyleInputs{VisualStyle: style}, Assets: []promptcompiler.AssetRef{{UploadID: "owned-fixture", StorageKey: "fixture.png", Role: role, MimeType: "image/png"}}}
				result, err := compileForTest(in)
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{"facial structure, proportions, hair and clothing", "unless explicitly changed", "natural believable motion", "coherent anatomy", "physically plausible lighting and material response", "temporal stability", "Preserve the selected visual style", "do not reinterpret it as live action", style} {
					if !strings.Contains(result.ContentItems[0].Text, want) {
						t.Errorf("provider text missing %q", want)
					}
				}
				expectedRole := modelark.RoleReferenceImage
				if role == "first_frame" {
					expectedRole = modelark.RoleFirstFrame
				}
				if len(result.ContentItems) != 2 || result.ContentItems[1].Role != expectedRole || result.ContentItems[1].URL != "fixture.png" || result.ContentItems[0].Text != result.CompiledPrompt || result.Snapshot.Assets[0].Role != role {
					t.Fatal("provider content or snapshot changed")
				}
			})
		}
	}
}

func TestTextOnlyVideoAndVideoReferenceDoNotGainImageControls(t *testing.T) {
	t.Parallel()
	for _, withVideo := range []bool{false, true} {
		in := promptcompiler.Inputs{ModelID: "violet-motion-2", Quality: "standard", Scene: &promptcompiler.SceneInputs{Description: "A boat drifts"}, Style: &promptcompiler.StyleInputs{VisualStyle: "clay"}}
		if withVideo {
			in.Assets = []promptcompiler.AssetRef{{Role: "reference", MimeType: "video/mp4", StorageKey: "fixture.mp4"}}
		}
		result, err := compileForTest(in)
		if err != nil {
			t.Fatal(err)
		}
		if result.CompiledPrompt != "A boat drifts. clay" {
			t.Fatalf("unrelated prompt changed: %q", result.CompiledPrompt)
		}
	}
}
