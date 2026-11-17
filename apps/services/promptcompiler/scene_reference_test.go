package promptcompiler

import (
	"strings"
	"testing"
)

func TestFilmedHumanSceneReferenceOptInPreservesPipelineControls(t *testing.T) {
	t.Parallel()
	asset := AssetRef{Role: "reference", MimeType: "image/png"}
	original := referenceControls([]AssetRef{asset}, PipelineFilmedHumanVideoVersion)
	expected := "Image 1: casting/design guide for face/body geometry, haircut and outfit only; no beautification or identity drift. Exclude its shading, CG materials, drawn skin, pose and background. Not a texture/style/start frame; follow scene setting, action and camera."
	if len(original) != 2 || original[0] != expected {
		t.Fatalf("default pipeline changed: %+v", original)
	}
	asset.PreserveScene = true
	scene := referenceControls([]AssetRef{asset}, PipelineFilmedHumanVideoVersion)
	if len(scene) != 2 || scene[1] != original[1] {
		t.Fatalf("shared appearance diverged: %+v", scene)
	}
	for _, want := range []string{"complete scene design and composition guide", "Preserve subject placement, pose, camera framing", "scene background/environment", "does not guarantee a pixel-exact start frame"} {
		if !strings.Contains(scene[0], want) {
			t.Fatal(scene)
		}
	}
	for _, bad := range []string{"pose and background", "outfit only", "Not a texture/style/start frame"} {
		if strings.Contains(scene[0], bad) {
			t.Fatal(scene)
		}
	}
}
