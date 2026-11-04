package productdesign

import (
	"strings"
	"testing"
)

func TestProductOptionsAndReferencePackshot(t *testing.T) {
	t.Parallel()
	for style := range styles {
		for mood := range moods {
			prompt, err := Compile("A glass bottle", Options{VisualStyle: style, Mood: mood}, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range []string{"one single product packshot", "plain neutral background", "existing labels", "Do not invent", "logos", "marketing claims", "A glass bottle"} {
				if !strings.Contains(prompt, required) {
					t.Fatalf("%s/%s missing %q", style, mood, required)
				}
			}
			for _, forbidden := range []string{"anatomy", "A-pose", "facial expression", "full-body", "turnaround", "portrait", "skin", "hands", "feet"} {
				if strings.Contains(prompt, forbidden) {
					t.Fatalf("character instruction leaked %q", forbidden)
				}
			}
		}
	}
	defaults, err := Normalize(Options{})
	if err != nil || defaults != (Options{VisualStyle: "auto", Mood: "neutral"}) {
		t.Fatalf("invalid defaults %+v %v", defaults, err)
	}
	for _, o := range []Options{{VisualStyle: "invalid"}, {Mood: "invalid"}} {
		if _, err := Compile("product", o, false); err == nil {
			t.Fatal("invalid option accepted")
		}
	}
}
