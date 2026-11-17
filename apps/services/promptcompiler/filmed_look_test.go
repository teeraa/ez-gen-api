package promptcompiler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

func TestFilmedSceneLooksAreDistinctPrioritizedAndKeepIdentity(t *testing.T) {
	t.Parallel()
	model, _ := modelark.ProviderModel("dreamina-seedance-2-0-fast-260128")
	prompts := map[FilmedLook]string{}
	for _, look := range []FilmedLook{FilmedLookNatural, FilmedLookCinematic} {
		in := Inputs{PipelineRenderingVersion: PipelineFilmedHumanVideoVersion, FilmedLook: look, ModelID: model.ProviderModelID, Quality: "standard", Scene: &SceneInputs{Description: strings.Repeat("the fictional character walks through the station. ", 150)}, Assets: []AssetRef{{StorageKey: "synthetic-scene", Role: "reference", MimeType: "image/png", PreserveScene: true}}}
		r, e := Compile(in, model)
		if e != nil {
			t.Fatal(e)
		}
		prompts[look] = r.CompiledPrompt
		for _, want := range []string{"recognizable facial identity, apparent age", "not a target for reproducing CGI rendering", "naturally occurring microtexture, subtle pores", "individually distinguishable strands", "subtle flyaways", "realistic fabric fibers", "natural human biomechanics", "identity drift"} {
			if !strings.Contains(r.CompiledPrompt, want) {
				t.Fatalf("%s control truncated/missing %q: %s", look, want, r.CompiledPrompt)
			}
		}
		if utf8.RuneCountInString(r.CompiledPrompt) > VideoPromptRuneLimit || r.Snapshot.FilmedLook != look || len(r.ContentItems) != 2 || r.ContentItems[1].Role != modelark.RoleReferenceImage {
			t.Fatalf("look changed budget/snapshot/reference semantics %+v", r)
		}
	}
	if prompts[FilmedLookNatural] == prompts[FilmedLookCinematic] || !strings.Contains(prompts[FilmedLookNatural], "REALISTIC MODE — AUTHENTIC OBSERVATIONAL FOOTAGE") || !strings.Contains(prompts[FilmedLookCinematic], "CINEMATIC MODE — PHOTOREALISTIC LIVE-ACTION CINEMATOGRAPHY") {
		t.Fatal("selected photographic directions are indistinguishable")
	}
}
func TestFilmedLookDoesNotChangeOtherConsumersOrInjectUnknownValues(t *testing.T) {
	t.Parallel()
	model, _ := modelark.ProviderModel("dreamina-seedance-2-0-fast-260128")
	for _, version := range []string{"", PipelineCinematicVideoVersion, PipelineFilmedHumanVideoVersion} {
		for _, preserve := range []bool{false, true} {
			base := Inputs{PipelineRenderingVersion: version, ModelID: model.ProviderModelID, Quality: "standard", Scene: &SceneInputs{Description: "a fictional character walks"}, Assets: []AssetRef{{Role: "reference", MimeType: "image/png", PreserveScene: preserve}}}
			original, e := Compile(base, model)
			if e != nil {
				t.Fatal(e)
			}
			invalid := base
			invalid.FilmedLook = FilmedLook("INJECT arbitrary direction")
			got, e := Compile(invalid, model)
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(original, got) {
				t.Fatal("unknown look changed compiled content or snapshot")
			}
			if version != PipelineFilmedHumanVideoVersion || !preserve {
				selected := base
				selected.FilmedLook = FilmedLookCinematic
				got, e = Compile(selected, model)
				if e != nil {
					t.Fatal(e)
				}
				if !reflect.DeepEqual(original, got) {
					t.Fatal("look escaped its opted-in filmed-scene boundary")
				}
			}
			encoded, _ := json.Marshal(original.Snapshot)
			if strings.Contains(string(encoded), "filmedLook") {
				t.Fatal("default snapshot gained look metadata")
			}
		}
	}
}
func TestFilmedLookDoesNotReinterpretExplicitFirstFrames(t *testing.T) {
	t.Parallel()
	in := Inputs{PipelineRenderingVersion: PipelineFilmedHumanVideoVersion, FilmedLook: FilmedLookCinematic, Assets: []AssetRef{{Role: "first_frame", MimeType: "image/png", PreserveScene: true}}}
	if activeFilmedLook(in) != "" {
		t.Fatal("first frame accidentally opted into scene reinterpretation")
	}
}
