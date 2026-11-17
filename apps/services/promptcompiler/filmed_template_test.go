package promptcompiler

import (
	"crypto/sha256"
	"fmt"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestUserFilmedTemplateSourceBlocksMatchExactUserText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, value, hash string
		length            int
	}{
		{"base", filmedBaseTemplate, "8c38957310bd90fa06ba1c187514177eadbe20fa8730ccba6cd567eff87b1ccd", 3893},
		{"realistic", filmedRealisticTemplate, "aa5e5f40b3134b2a27b133f557b26926b413421f37890f807b658ca84c8680e4", 1467},
		{"cinematic", filmedCinematicTemplate, "6ab4f8b8443bb4b225fcd71340b4d9c71e68b6ff40ecf82c182b97d035b101ef", 1989},
		{"negative", filmedNegativeTemplate, "23f57d5542d5c32b710cd24126199e3f7ad89b0b8618e80525b6e238bffc863f", 1030},
	}
	for _, c := range cases {
		if fmt.Sprintf("%x", sha256.Sum256([]byte(c.value))) != c.hash || utf8.RuneCountInString(c.value) != c.length {
			t.Fatalf("user block %s was changed", c.name)
		}
	}
}
func TestFullUserFilmedTemplateReachesVideoWithoutClipping(t *testing.T) {
	t.Parallel()
	model, _ := modelark.ProviderModel("dreamina-seedance-2-0-fast-260128")
	for _, look := range []FilmedLook{FilmedLookNatural, FilmedLookCinematic} {
		scene := "SHOT: preserve the station at dusk. " + strings.Repeat("ตัวละครเดินผ่านสถานี ", 100) + "SHOT_TAIL_ครบ🙂"
		in := Inputs{PipelineRenderingVersion: PipelineFilmedHumanVideoVersion, FilmedLook: look, Scene: &SceneInputs{Description: scene}, Assets: []AssetRef{{Role: "reference", MimeType: "image/png", PreserveScene: true}}}
		r, e := Compile(in, model)
		if e != nil {
			t.Fatal(e)
		}
		want := filmedLead + "\n\n" + filmedTemplateHead(look, 1) + "\n\nShot:\n" + scene + "\n\n" + filmedNegativeSection
		if r.CompiledPrompt != want || r.ContentItems[0].Text != r.CompiledPrompt {
			t.Fatalf("filmed layout must be template head, separated shot, then negative constraints:\n%s", r.CompiledPrompt)
		}
		if len(r.ContentItems) != 2 || r.ContentItems[1].Role != modelark.RoleReferenceImage {
			t.Fatal("template changed image role")
		}
		used, limit := PromptUsage(in, model)
		if used != utf8.RuneCountInString(r.CompiledPrompt) || used > limit {
			t.Fatal("usage differs from exact full template")
		}
	}
}
func TestFullUserTemplateReferenceIndexAdaptation(t *testing.T) {
	t.Parallel()
	got := filmedTemplate(FilmedLookCinematic, 2)
	if !strings.HasPrefix(got, "Image 2 is the primary reference") || strings.Contains(got, "Image 1 is the primary reference") || !strings.Contains(got, filmedCinematicTemplate) || !strings.HasSuffix(got, filmedNegativeTemplate) {
		t.Fatal("reference index adaptation changed unrelated user text")
	}
}

func TestFilmedLayoutKeepsShotOutOfNegativeConstraints(t *testing.T) {
	t.Parallel()
	model, _ := modelark.ProviderModel("dreamina-seedance-2-0-fast-260128")
	for _, look := range []FilmedLook{FilmedLookNatural, FilmedLookCinematic} {
		in := Inputs{PipelineRenderingVersion: PipelineFilmedHumanVideoVersion, FilmedLook: look, Scene: &SceneInputs{Description: "Mali walks to the stall.\nCamera: medium shot", SettingOverride: "A rainy market at dusk."}, Assets: []AssetRef{{Role: "reference", MimeType: "image/png", PreserveScene: true}}}
		r, e := Compile(in, model)
		if e != nil {
			t.Fatal(e)
		}
		p := r.CompiledPrompt
		head, shot, neg := strings.Index(p, filmedTemplateHead(look, 1)), strings.Index(p, "\n\nShot:\nScene environment: A rainy market at dusk.\nMali walks to the stall.\nCamera: medium shot\n\n"), strings.Index(p, filmedNegativeSection)
		if !strings.HasPrefix(p, filmedLead+"\n\n") || head != len(filmedLead)+2 || shot <= head || neg <= shot || !strings.HasSuffix(p, filmedNegativeSection) || strings.Contains(p, "..") || strings.Count(p, "Negative constraints:") != 1 {
			t.Fatalf("shot context must sit between the mode block and the negative list:\n%s", p)
		}
		if strings.Contains(p, filmedRealisticTemplate) == (look == FilmedLookCinematic) || strings.Contains(p, filmedCinematicTemplate) == (look == FilmedLookNatural) {
			t.Fatal("only the selected mode block may be present")
		}
		used, _ := PromptUsage(in, model)
		if used != utf8.RuneCountInString(p) {
			t.Fatal("usage must match the composed layout")
		}
	}
}

func TestNonFilmedPromptLayoutUnchanged(t *testing.T) {
	t.Parallel()
	model, _ := modelark.ProviderModel("dreamina-seedance-2-0-fast-260128")
	in := Inputs{PipelineRenderingVersion: PipelineFilmedHumanVideoVersion, Scene: &SceneInputs{Description: "walks", SettingOverride: "market"}, Assets: []AssetRef{{Role: "reference", MimeType: "image/png", PreserveScene: true}}}
	r, e := Compile(in, model)
	if e != nil {
		t.Fatal(e)
	}
	controls, sections, _ := assemblePrompt(in)
	if r.CompiledPrompt != strings.Join(append(controls, sections...), ". ") || strings.Contains(r.CompiledPrompt, "Shot:\n") {
		t.Fatal("prompts without the filmed template must keep the previous join")
	}
}

func TestPoseCastingLayoutTakesEnvironmentFromText(t *testing.T) {
	t.Parallel()
	model, _ := modelark.ProviderModel("dreamina-seedance-2-0-fast-260128")
	for _, look := range []FilmedLook{FilmedLookNatural, FilmedLookCinematic} {
		in := Inputs{PipelineRenderingVersion: PipelineFilmedHumanVideoVersion, FilmedLook: look, Scene: &SceneInputs{Description: "He holds her in his arms.\nCamera: wide shot", SettingOverride: "Rural courtyard, overcast late afternoon."}, Assets: []AssetRef{{Role: "reference", MimeType: "image/png", PoseCasting: true}}}
		r, e := Compile(in, model)
		if e != nil {
			t.Fatal(e)
		}
		p := r.CompiledPrompt
		casting, human, shot, neg := strings.Index(p, "Image 1: casting and pose guide"), strings.Index(p, filmedHumanControl), strings.Index(p, "\n\nShot:\nScene environment: Rural courtyard, overcast late afternoon.\nHe holds her in his arms.\nCamera: wide shot\n\n"), strings.Index(p, filmedNegativeSection)
		if !strings.HasPrefix(p, filmedLead+"\n\n") || casting < 0 || human < casting || shot < human || neg < shot || !strings.HasSuffix(p, filmedNegativeSection) {
			t.Fatalf("pose casting layout wrong:\n%s", p)
		}
		if strings.Contains(p, "Image 1 is the primary reference") || strings.Contains(p, "Preserve the reference image's broad framing, composition, environment") {
			t.Fatal("base block would tell the model to keep the gray casting background")
		}
		if strings.Contains(p, filmedRealisticTemplate) != (look == FilmedLookNatural) || strings.Contains(p, filmedCinematicTemplate) != (look == FilmedLookCinematic) {
			t.Fatal("only the selected mode block may be present")
		}
		if used, _ := PromptUsage(in, model); used != utf8.RuneCountInString(p) {
			t.Fatal("usage must match the composed layout")
		}
	}
}
