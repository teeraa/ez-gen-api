package promptcompiler

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

func TestVideoPromptBudgetPreservesRepresentativeLongTemplate(t *testing.T) {
	t.Parallel()
	model, _ := modelark.ProviderModel("dreamina-seedance-2-0-fast-260128")
	base := strings.Repeat("BASE: preserve the fictional character, scene and photographic intent. ", 65)
	mode := strings.Repeat("MODE: natural materials, subtle motion, scene-consistent illumination. ", 35)
	shot := strings.Repeat("SHOT: ตัวละครเดินและพูดอย่างเป็นธรรมชาติในฉากเดิม ", 35) + "ท้ายฉากครบถ้วน🙂"
	negative := strings.Repeat("no unrelated subjects, no scene reset, no beauty smoothing; ", 18) + "NEGATIVE_TAIL_ครบ🙂"
	scene := base + mode + shot
	in := Inputs{Scene: &SceneInputs{Description: scene}, Tags: []models.DNATag{{Category: models.TagCategoryNegativeConstraint, Name: negative}}, Assets: []AssetRef{{Role: "reference", MimeType: "image/png", PreserveScene: true}}}
	r, e := Compile(in, model)
	if e != nil {
		t.Fatal(e)
	}
	used, limit := PromptUsage(in, model)
	if used < 8000 || used > 16000 || limit != VideoPromptRuneLimit || used != utf8.RuneCountInString(r.CompiledPrompt) {
		t.Fatalf("fixture size/usage mismatch used=%d limit=%d", used, limit)
	}
	if !strings.Contains(r.CompiledPrompt, scene) || !strings.Contains(r.CompiledPrompt, negative) || r.NegativeInstructions != negative || !strings.HasSuffix(r.CompiledPrompt, "ท้ายฉากครบถ้วน🙂") || r.ContentItems[0].Text != r.CompiledPrompt || !utf8.ValidString(r.CompiledPrompt) {
		t.Fatal("long video template/control/Thai tail was clipped")
	}
}
func TestVideoPromptApplicationBudgetExactAndOverBoundary(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"seedance-1-0-pro-250528", "dreamina-seedance-2-0-fast-260128", "dreamina-seedance-2-0-260128", "dreamina-seedance-2-5-260628"} {
		model, _ := modelark.ProviderModel(provider)
		for _, withDialogue := range []bool{false, true} {
			if withDialogue && !model.SupportsAudio {
				continue
			}
			t.Run(provider+map[bool]string{false: "/text", true: "/dialogue"}[withDialogue], func(t *testing.T) {
				in := Inputs{Scene: &SceneInputs{Description: "x"}, Assets: []AssetRef{{Role: "reference", MimeType: "image/png"}}}
				if withDialogue {
					in.Dialogue = "สวัสดีครับ \"ทุกคน\"🙂"
				}
				used, limit := PromptUsage(in, model)
				in.Scene.Description = strings.Repeat("ก", limit-used+1)
				r, e := Compile(in, model)
				if e != nil {
					t.Fatal(e)
				}
				actual, want := PromptUsage(in, model)
				if actual != want || utf8.RuneCountInString(r.CompiledPrompt) != limit {
					t.Fatal("exact boundary not accepted consistently")
				}
				in.Scene.Description += "🙂"
				if used, limit := PromptUsage(in, model); used != limit+1 {
					t.Fatal("Unicode over-boundary count incorrect")
				}
				if _, e := Compile(in, model); !errors.Is(e, ErrPromptBudget) {
					t.Fatalf("overbudget silently truncated: %v", e)
				}
			})
		}
	}
}
