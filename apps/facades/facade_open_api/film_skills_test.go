package facade_open_api

import (
	"strings"
	"testing"
)

func TestFilmSkillsTextInPlanPrompts(t *testing.T) {
	t.Parallel()
	if got := filmSkillsText(nil); got != "" {
		t.Fatalf("no skills must add nothing, got %q", got)
	}
	skills := []filmPlanSkill{{Name: "ฟิล์มนัวร์", Directive: "Low-key lighting from one hard source."}}
	text := filmSkillsText(skills)
	if !strings.Contains(text, `Skill "ฟิล์มนัวร์": Low-key lighting`) || !strings.Contains(text, "one movement per shot") {
		t.Fatalf("skills text: %q", text)
	}
	plan := filmPlanJob{Prompt: "เรื่องนักสืบ", TargetSeconds: 30, ShotIndex: -1, Style: "cinematic", Filmed: true, Ratio: "16:9", Skills: skills}
	if p := filmPlanPrompt(&plan); !strings.Contains(p, text) {
		t.Fatal("whole-episode plan prompt misses the chosen skills")
	}
	ops := filmPlanJob{Mode: filmPlanModeOps, Prompt: "ฉาก 1 · ช็อต 1 ให้มืดลง", ShotIndex: -1, Style: "cinematic", Ratio: "16:9", Skills: skills}
	if p := filmOpsPrompt(&ops); !strings.Contains(p, text) {
		t.Fatal("position-edit prompt misses the chosen skills")
	}
	plain := filmPlanJob{Prompt: "เรื่องนักสืบ", TargetSeconds: 30, ShotIndex: -1, Style: "anime", Ratio: "16:9"}
	if strings.Contains(filmPlanPrompt(&plain), "Director skills") {
		t.Fatal("an episode without skills must not mention them")
	}
}

func TestNormalizeSkillDirective(t *testing.T) {
	t.Parallel()
	base := SkillInput{Name: "มุมกล้อง", Steps: []string{"a"}, Output: SkillOutputFilm}
	if _, err := normalizeSkill(base); err == nil {
		t.Fatal("a film skill without a directive must be rejected")
	}
	base.Directive = "  Static frames, slow push ins only.  "
	got, err := normalizeSkill(base)
	if err != nil || got.Directive != "Static frames, slow push ins only." {
		t.Fatalf("film skill: %+v %v", got, err)
	}
	base.Output = "video_9_16"
	if got, err = normalizeSkill(base); err != nil || got.Directive != "" {
		t.Fatalf("non-film skills drop the directive: %+v %v", got, err)
	}
}
