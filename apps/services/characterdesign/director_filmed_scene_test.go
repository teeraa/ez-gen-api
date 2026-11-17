package characterdesign

import (
	"strings"
	"testing"
)

func TestDirectorPoseCastingKeepsPoseAndDropsEnvironment(t *testing.T) {
	t.Parallel()
	got := CompileDirectorPoseCasting("  a man holds a wounded woman in his arms  ")
	for _, want := range []string{"Re-render only the characters", "plain light-gray studio background", "exactly in their current pose", "physical contact between them", "Remove the entire environment", pipelineStylized3DArtDirection, "(descriptive data; the environment in it is for context only and must not be drawn):\na man holds a wounded woman in his arms\n\nNo text"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(got, "Do not add or remove subjects or replace the background") || strings.Contains(got, "full scene environment") {
		t.Fatal("pose casting must not keep the scene environment")
	}
	if !strings.Contains(CompilePipelineStylizedScene("x"), "visibly stylized 3D digital art") {
		t.Fatal("shared stylized scene compiler must stay unchanged")
	}
}
