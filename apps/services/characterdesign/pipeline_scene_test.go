package characterdesign

import (
	"strings"
	"testing"
)

func TestPipelineStylizedScenePreservesComposition(t *testing.T) {
	t.Parallel()
	scene := CompilePipelineStylizedScene("two characters talking on a train")
	original, e := CompilePipelineStylized3D("actor", Options{VisualStyle: "cinematic_realistic"}, false)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(scene, pipelineStylized3DArtDirection) || !strings.Contains(original, pipelineStylized3DArtDirection) {
		t.Fatal("scene and character render treatment diverged")
	}
	for _, want := range []string{"Preserve every visible character", "full scene environment", "two characters talking on a train"} {
		if !strings.Contains(scene, want) {
			t.Fatal(scene)
		}
	}
	for _, bad := range []string{"Plain light-gray studio background", "one fictional character reference", "single full-body", "No typography, logos, watermarks or scene staging"} {
		if strings.Contains(scene, bad) {
			t.Fatalf("scene lost composition: %s", scene)
		}
	}
}
