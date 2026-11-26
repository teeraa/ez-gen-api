package facade_open_api

import (
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
)

func TestFilmBlockingInPromptsAndHash(t *testing.T) {
	t.Parallel()
	hero := models.FilmElement{Base: models.Base{ID: uuid.New()}, Kind: "character", Name: "มิว", Spec: "Thai woman, 27"}
	byID := filmElementMap([]models.FilmElement{hero})
	id := hero.ID.String()
	sh := &filmShot{ID: "s", Seconds: 5, Transition: "cut", CharacterIDs: []string{id}, Action: "@มิว types", Camera: "medium shot"}
	before := filmShotHash(sh, byID, "realistic")
	b := &filmBrief{Shot: sh, Cast: []*models.FilmElement{byID[id]}, Style: "realistic", Filmed: true}
	if p := filmReferencePrompt(b); strings.Contains(p, "[Blocking]") || !strings.Contains(p, "[Continuity]") {
		t.Fatalf("no blocking line without blocking, continuity always:\n%s", p)
	}
	sh.Blocking = "@มิว: screen-left, seated at her desk, facing screen-right"
	if filmShotHash(sh, byID, "realistic") == before {
		t.Fatal("blocking changes the shot hash")
	}
	p := filmReferencePrompt(b)
	if i := strings.Index(p, "[Blocking]"); i < 0 || i > strings.Index(p, "Reference images:") || !strings.Contains(p, "seated at her desk") || !strings.Contains(p, "standing identity photos only") {
		t.Fatalf("blocking leads the reference prompt:\n%s", p)
	}
	if c := filmContinuePrompt(b); !strings.Contains(c, "[Blocking]") {
		t.Fatalf("continue prompt keeps blocking:\n%s", c)
	}
	sh.Blocking = ""
	if filmShotHash(sh, byID, "realistic") != before {
		t.Fatal("shots without blocking keep their old hash (no stale clips)")
	}
}

func TestFilmPlanCarriesBlockingInsideAScene(t *testing.T) {
	t.Parallel()
	resolve := func(kind, name string) string {
		if kind == "location" && name == "office" {
			return "loc"
		}
		return ""
	}
	plan := []filmPlanShot{
		{Scene: 1, Seconds: 5, Location: "office", Action: "a", Blocking: "@มิว: screen-left, seated"},
		{Scene: 1, Seconds: 5, Location: "office", Action: "b"},
		{Scene: 2, Seconds: 5, Location: "office", Action: "c"},
	}
	shots, _ := filmPlanShots(plan, nil, &filmDocument{}, resolve)
	if shots[1].Blocking != "@มิว: screen-left, seated" || shots[2].Blocking != "" {
		t.Fatalf("blocking carries within a scene only: %+v", shots)
	}
	if !strings.Contains(filmPlanRulesText(), "180-degree rule") {
		t.Fatal("planner rules ask for blocking and the 180-degree rule")
	}
}
