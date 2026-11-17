package facade_open_api

import (
	"strings"
	"testing"
)

func TestDirectorNormalizePropsTrimsDedupesAndCaps(t *testing.T) {
	t.Parallel()
	in := []directorProp{{Name: " Jade sword ", Spec: "straight double-edged jian, one hilt"}, {Name: "jade SWORD", Spec: "dup"}, {Name: "", Spec: "x"}, {Name: "umbrella", Spec: ""}}
	for i := 0; i < 10; i++ {
		in = append(in, directorProp{Name: "prop" + strings.Repeat("x", i), Spec: "spec"})
	}
	out := directorNormalizeProps(in)
	if len(out) != directorMaxProps || out[0].Name != "Jade sword" {
		t.Fatalf("props: %+v", out)
	}
}

func TestDirectorPropLinesOnlyNamedPropsPlusIntegrity(t *testing.T) {
	t.Parallel()
	d := directorDocument{Props: []directorProp{{Name: "jade sword", Spec: "one hilt at one end, scabbard separate"}, {Name: "oil-paper umbrella", Spec: "single straight shaft, one handle"}}}
	got := directorPropLines(&d, "Li Feng raises the Jade Sword in his right hand")
	if !strings.Contains(got, "one hilt at one end") || strings.Contains(got, "single straight shaft") {
		t.Fatalf("named props: %q", got)
	}
	if !strings.Contains(got, directorObjectIntegrity) || !strings.Contains(got, directorFaceIntegrity) {
		t.Fatalf("integrity rules missing: %q", got)
	}
	d.Props = nil
	if got := directorPropLines(&d, "anything"); !strings.Contains(got, directorObjectIntegrity) {
		t.Fatalf("integrity without props: %q", got)
	}
}

func TestDirectorVideoPromptsCarryPropStructure(t *testing.T) {
	t.Parallel()
	d := directorDocument{Project: directorTestProject(), Props: []directorProp{{Name: "sword", Spec: "one hilt at one end only"}}}
	sh := &d.Project.Scenes[0].Shots[0]
	sh.Description = "He draws the sword"
	for _, prompt := range []string{directorVideoPrompt(&d, &d.Project, sh, nil, false), directorExtensionPrompt(&d, &d.Project, sh, nil, 5), directorImagePrompt(&d, &d.Project, sh, nil, 0)} {
		if !strings.Contains(prompt, "one hilt at one end only") || !strings.Contains(prompt, directorFaceIntegrity) {
			t.Fatalf("prompt lacks prop structure: %q", prompt)
		}
	}
}
