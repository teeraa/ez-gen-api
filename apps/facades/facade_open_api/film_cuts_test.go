package facade_open_api

import (
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
)

func filmCutsShot(hero, camp string) filmShot {
	return filmShot{ID: "s", Seconds: 9, Transition: "cut", CharacterIDs: []string{hero}, LocationID: camp, Action: "A three-cut duel fragment.", Camera: "Fast trailer cutting.", Dialogue: []filmLine{},
		Cuts: []filmCut{
			{Camera: "Extreme wide, 24mm, static.", Action: "She lands on the bridge, knees bending under her weight. <boots thud on wet wood>"},
			{Camera: "Close-up, 85mm, handheld.", Action: "Her blade cuts across frame. <dry steel ring>"},
			{Camera: "Insert, 50mm, locked-off.", Action: "A drop of rain splits on the edge."},
		}}
}

func TestFilmMultiShotPromptsListEveryCut(t *testing.T) {
	t.Parallel()
	els, hero, _, camp, _ := filmTestElements()
	for _, filmed := range []bool{true, false} {
		sh := filmCutsShot(hero, camp)
		style := "anime"
		if filmed {
			style = "realistic"
		}
		p := filmReferencePrompt(&filmBrief{Shot: &sh, Cast: []*models.FilmElement{els[hero]}, Location: els[camp], Style: style, Filmed: filmed})
		if !strings.Contains(p, "MULTI-SHOT SEQUENCE") || !strings.Contains(p, "Shot 1: Extreme wide") || !strings.Contains(p, "Shot 3 (hard cut): Insert") {
			t.Fatalf("filmed=%t: every cut in order: %.600s", filmed, p)
		}
		if strings.Contains(p, "SHOT (follow exactly)") || strings.Contains(p, filmLockedCamera) {
			t.Fatalf("filmed=%t: no single-camera lead or whole-clip lock in a sequence", filmed)
		}
		if strings.Count(p, "Camera locked off for this shot.") != 2 {
			t.Fatalf("filmed=%t: static cuts are locked one by one", filmed)
		}
		if filmed && !strings.HasPrefix(p, "MULTI-SHOT SEQUENCE") {
			t.Fatalf("live action leads with the sequence: %.120s", p)
		}
	}
}

func TestFilmCutsChangeTheHashOnlyWhenPresent(t *testing.T) {
	t.Parallel()
	els, hero, _, camp, _ := filmTestElements()
	sh := filmCutsShot(hero, camp)
	plain := sh
	plain.Cuts = nil
	if filmShotHash(&sh, els, "realistic") == filmShotHash(&plain, els, "realistic") {
		t.Fatal("cuts are part of the clip")
	}
	empty := plain
	empty.Cuts = []filmCut{}
	if filmShotHash(&plain, els, "realistic") != filmShotHash(&empty, els, "realistic") {
		t.Fatal("a shot without cuts keeps its old hash")
	}
}

func TestFilmCutsValidation(t *testing.T) {
	t.Parallel()
	two := []req.FilmCutRequest{{Camera: "Wide", Action: "a"}, {Action: "b"}}
	if c, ok := filmCutsFrom(two); !ok || len(c) != 2 {
		t.Fatalf("two cuts are a sequence: %v %v", c, ok)
	}
	if c, ok := filmCutsFrom(nil); !ok || c != nil {
		t.Fatal("no cuts is a plain shot")
	}
	nine := make([]req.FilmCutRequest, filmMaxCuts+1)
	for i := range nine {
		nine[i].Action = "x"
	}
	for _, bad := range [][]req.FilmCutRequest{{{Action: "one"}}, nine, {{Action: "a"}, {Action: "  "}}} {
		if _, ok := filmCutsFrom(bad); ok {
			t.Fatalf("invalid cuts accepted: %d", len(bad))
		}
	}
	if filmPlanCuts([]filmPlanCut{{Action: "only one"}, {Action: " "}}) != nil {
		t.Fatal("the planner's single cut is just a shot")
	}
	if got := filmPlanCuts([]filmPlanCut{{Camera: "a", Action: "x"}, {Camera: "b", Action: "y"}}); len(got) != 2 || got[1].Camera != "b" {
		t.Fatalf("planner cuts kept: %+v", got)
	}
}
