package facade_open_api

import (
	"strings"
	"testing"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

func TestDirectorModelLabel(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"seedream-5-0-260128":   "Seedream 5.0",
		"gemini-3.5-flash":      "Gemini 3.5 Flash",
		"gemini-3.5-flash-lite": "Gemini 3.5 Flash Lite",
		"":                      "มาตรฐาน",
		"custom-endpoint":       "custom-endpoint",
	}
	for in, want := range cases {
		if got := directorModelLabel(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestDirectorVideoModelsIgnoreRegistryOverride(t *testing.T) {
	t.Parallel()
	registry, err := modelark.NewRegistry("seedance-1-0-pro-250528")
	if err != nil {
		t.Fatal(err)
	}
	f := NewDirectorFacade(nil, nil, nil, nil, nil, registry, modelark.NewMockClient(), nil, DirectorOptions{Enabled: true, AnalysisModel: "gemini-3.5-flash", ImageModel: "seedream-5-0-260128", StoryPoints: 4, ImagePoints: 3, VideoPoints: 20})
	c := f.Capabilities()
	want := []struct{ id, name string }{{"seedance-2-0-fast", "Seedance 2.0 Fast"}, {"seedance-2-5", "Seedance 2.5"}, {"seedance-2-0", "Seedance 2.0"}}
	if len(c.Models.Video) != len(want) {
		t.Fatalf("video models: %+v", c.Models.Video)
	}
	for i, w := range want {
		if c.Models.Video[i].ID != w.id || c.Models.Video[i].Name != w.name {
			t.Fatalf("video model %d: %+v", i, c.Models.Video[i])
		}
	}
	if c.Models.Story[0].Name != "Gemini 3.5 Flash" || c.Models.Image[0].Name != "Seedream 5.0" {
		t.Fatalf("labels: %+v %+v", c.Models.Story, c.Models.Image)
	}
	if m, ok := directorVideoModel("violet-motion-2"); !ok || m.ProviderModelID != "dreamina-seedance-2-0-fast-260128" {
		t.Fatalf("legacy id: %+v %v", m, ok)
	}
}

func TestDirectorAlignPlannedShotsTolerantOutput(t *testing.T) {
	t.Parallel()
	f := NewDirectorFacade(nil, nil, nil, nil, nil, nil, nil, nil, DirectorOptions{})
	var in struct {
		Shots []directorShotOutput `json:"shots"`
	}
	// Renumbered IDs, a missing characterAssetIds field, an unlisted camera and an extra field.
	text := "```json\n{\"shots\":[{\"id\":\"1\",\"description\":\"เด็กเดินในป่า\",\"camera\":\"กล้องมุมกว้างมาก\",\"note\":\"x\"},{\"id\":\"2\",\"description\":\"เด็กมองดาว\",\"camera\":\"โคลสอัพ\",\"characterAssetIds\":[\"unknown\"]}]}\n```"
	if err := directorDecodeJSON(text, &in); err != nil {
		t.Fatal(err)
	}
	p := directorTestProject()
	got := f.alignPlannedShots(in.Shots, &p)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" || got[0].Camera != "มุมกว้าง" || got[1].Camera != "โคลสอัพ" || got[0].CharacterAssetIDs == nil || len(got[1].CharacterAssetIDs) != 0 {
		t.Fatalf("aligned: %+v", got)
	}
}

func directorTestProject() dto.DirectorProject {
	return dto.DirectorProject{Scenes: []dto.DirectorScene{{ID: "s1", CharacterAssetIDs: []string{}, Shots: []dto.DirectorShot{{ID: "a", SceneID: "s1"}, {ID: "b", SceneID: "s1"}}}}}
}

func TestDirectorImagePromptBindsCastSettingAndStyle(t *testing.T) {
	t.Parallel()
	p := directorTestProject()
	p.Style, p.AspectRatio, p.Idea.Genre = "cinematic", "16:9", "fantasy"
	p.Scenes[0].Title, p.Scenes[0].Summary = "ลานประลอง", "สองสำนักเผชิญหน้า"
	p.Scenes[0].Shots[0].Description, p.Scenes[0].Shots[0].Camera = "หลินและเฟิงจ้องตากัน", "โคลสอัพ"
	d := directorDocument{Settings: map[string]string{"s1": "stone courtyard at dusk, red lanterns"}, Bible: "Song dynasty wuxia, ink-wash palette", Prompts: map[string]directorPlannedPrompt{"a": {Prompt: "Lin and Feng lock eyes", Hash: directorShotHash("หลินและเฟิงจ้องตากัน", "โคลสอัพ")}}}
	cast := []directorCharacter{{ID: "c1", Name: "หลิน", Description: "white robe"}, {ID: "c2", Name: "เฟิง", Description: "black armor"}}
	got := directorImagePrompt(&d, &p, &p.Scenes[0].Shots[0], cast, 2)
	for _, want := range []string{"close-up shot", "Lin and Feng lock eyes", "หลิน", "reference Image 1", "เฟิง", "reference Image 2", "stone courtyard at dusk", "Song dynasty wuxia", "cinematic film still"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q:\n%s", want, got)
		}
	}
	p.Scenes[0].Shots[0].Description = "ผู้ใช้แก้คำบรรยายเอง"
	if got = directorImagePrompt(&d, &p, &p.Scenes[0].Shots[0], cast, 2); strings.Contains(got, "Lin and Feng lock eyes") || !strings.Contains(got, "ผู้ใช้แก้คำบรรยายเอง") {
		t.Errorf("user edit must replace the planner prompt:\n%s", got)
	}
}
