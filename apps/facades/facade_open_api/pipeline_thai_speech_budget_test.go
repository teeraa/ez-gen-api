package facade_open_api

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
)

func TestPipelineThaiSpeechBudgetMatchesCompilerPreflight(t *testing.T) {
	for _, over := range []bool{false, true} {
		t.Run(map[bool]string{false: "at_limit", true: "over_limit"}[over], func(t *testing.T) {
			f, memory, images, videos, user, in := pipelineFixture(t)
			in.ModelID = "violet-realism"
			in.Dialogue = "สวัสดี \"ทุกคน\"\nWelcome"
			in.ScenePrompt = "x"
			registry, _ := modelark.NewRegistry("")
			model, _ := registry.Lookup(in.ModelID)
			result, e := promptcompiler.Compile(promptcompiler.Inputs{PipelineRenderingVersion: pipelineRenderingVersion(in.VisualStyle), Dialogue: in.Dialogue, ModelID: in.ModelID, Quality: in.Quality, Scene: &promptcompiler.SceneInputs{Description: in.ScenePrompt}, Assets: []promptcompiler.AssetRef{{Role: "reference", MimeType: "image/png", StorageKey: "generated/image.png"}}}, model)
			if e != nil {
				t.Fatal(e)
			}
			length := 2000 - utf8.RuneCountInString(result.CompiledPrompt) + 1
			if over {
				length++
			}
			in.ScenePrompt = strings.Repeat("ก", length)
			_, e = f.Create(context.Background(), user, in)
			if !over {
				if e != nil {
					t.Fatalf("valid exact-boundary request rejected: %v", e)
				}
				return
			}
			var validation *PipelineValidationError
			if !errors.As(e, &validation) || validation.Field != "scenePrompt" || validation.Reason != "prompt_budget" {
				t.Fatalf("missing actionable preflight %v", e)
			}
			if memory.p != nil || images.calls != 0 || videos.calls != 0 {
				t.Fatal("oversize request created work")
			}
		})
	}
}
