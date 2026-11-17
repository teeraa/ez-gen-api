package facade_open_api

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/google/uuid"
)

func filmTestElements() (map[string]*models.FilmElement, string, string, string, string) {
	img := func(s string) *string { return &s }
	hero := models.FilmElement{Base: models.Base{ID: uuid.New()}, Kind: "character", Name: "ขุนอินทร์", Spec: "Thai officer, 32, red waist sash", ImageKey: img("film/hero.png"), SyntheticKey: img("film/hero-casting.png")}
	boy := models.FilmElement{Base: models.Base{ID: uuid.New()}, Kind: "character", Name: "ทหารหนุ่ม", Spec: "young soldier, 20, cotton shirt", ImageKey: img("film/boy.png")}
	camp := models.FilmElement{Base: models.Base{ID: uuid.New()}, Kind: "location", Name: "แนวค่ายอยุธยา", Spec: "wooden rampart at dawn, smoke", ImageKey: img("film/camp.png")}
	sword := models.FilmElement{Base: models.Base{ID: uuid.New()}, Kind: "prop", Name: "ดาบไทย", Spec: "single hilt, separate wooden scabbard", ImageKey: img("film/sword.png")}
	m := filmElementMap([]models.FilmElement{hero, boy, camp, sword})
	return m, hero.ID.String(), boy.ID.String(), camp.ID.String(), sword.ID.String()
}

func TestFilmValidateShots(t *testing.T) {
	t.Parallel()
	els, hero, boy, camp, sword := filmTestElements()
	valid := req.FilmShotRequest{Seconds: 8, Transition: "cut", CharacterIDs: []string{boy, hero}, LocationID: camp, PropIDs: []string{sword}, Action: "runs up the steps", Camera: "tracking, then over-the-shoulder", Dialogue: []req.FilmLineRequest{{SpeakerID: boy, Line: "ท่านขุน! ข้าศึกมากันเต็มทุ่งแล้ว!", Delivery: "breathless"}}}
	shots, err := filmValidateShots([]req.FilmShotRequest{valid, {Seconds: 5, Transition: "continue", CharacterIDs: []string{hero}, Action: "turns his head"}}, nil, els, "cinematic")
	if err != nil || len(shots) != 2 || shots[0].ID == "" || shots[1].Transition != "continue" {
		t.Fatalf("valid list: %+v %v", shots, err)
	}
	cases := map[string]req.FilmShotRequest{
		"shots.transition":   {Seconds: 5, Transition: "continue", Action: "x"},
		"shots.seconds":      {Seconds: 3, Action: "x"},
		"shots.dialogue":     {Seconds: 5, CharacterIDs: []string{hero}, Action: "x", Dialogue: []req.FilmLineRequest{{SpeakerID: boy, Line: "not in shot"}}},
		"shots.locationId":   {Seconds: 5, LocationID: hero, Action: "x"},
		"shots.characterIds": {Seconds: 5, CharacterIDs: []string{camp}, Action: "x"},
		"shots.action":       {Seconds: 5, Action: "  "},
	}
	for field, shot := range cases {
		if _, err := filmValidateShots([]req.FilmShotRequest{shot}, nil, els, "anime"); !isValidation(err, field) {
			t.Fatalf("%s: got %v", field, err)
		}
	}
}

func TestFilmBusyShotCannotChange(t *testing.T) {
	t.Parallel()
	els, hero, _, _, _ := filmTestElements()
	current := []filmShot{{ID: uuid.NewString(), Seconds: 5, Transition: "cut", CharacterIDs: []string{hero}, PropIDs: []string{}, Dialogue: []filmLine{}, Action: "stands", Video: &filmVideo{Status: "processing", JobID: "j"}}}
	same := req.FilmShotRequest{ID: current[0].ID, Seconds: 5, Transition: "cut", CharacterIDs: []string{hero}, Action: "stands"}
	out, err := filmValidateShots([]req.FilmShotRequest{same}, current, els, "anime")
	if err != nil || out[0].Video == nil || out[0].Video.JobID != "j" {
		t.Fatalf("unchanged busy shot must keep its video: %+v %v", out, err)
	}
	changed := same
	changed.Action = "sits"
	var d *DirectorError
	if _, err = filmValidateShots([]req.FilmShotRequest{changed}, current, els, "anime"); !errors.As(err, &d) || d.Code != "SHOT_BUSY" {
		t.Fatalf("changed busy shot: %v", err)
	}
	if _, err = filmValidateShots(nil, current, els, "anime"); !errors.As(err, &d) || d.Code != "SHOT_BUSY" {
		t.Fatalf("removed busy shot: %v", err)
	}
}

func TestFilmReferencesUseSyntheticCharactersInFilmedStyles(t *testing.T) {
	t.Parallel()
	els, hero, boy, camp, sword := filmTestElements()
	sh := &filmShot{Seconds: 8, Transition: "cut", CharacterIDs: []string{boy, hero}, LocationID: camp, PropIDs: []string{sword}, Action: "runs up the steps", Camera: "tracking", Dialogue: []filmLine{{SpeakerID: boy, Line: "ท่านขุน!"}}, Audio: "footsteps on timber"}
	brief := filmBrief{Shot: sh, Cast: []*models.FilmElement{els[boy], els[hero]}, Location: els[camp], Props: []*models.FilmElement{els[sword]}, Style: "cinematic", Filmed: true}
	refs := filmReferences(&brief)
	if len(refs) != 4 || refs[1].Key != "film/hero-casting.png" || refs[2].Key != "film/camp.png" || refs[0].Label != "@ทหารหนุ่ม" {
		t.Fatalf("filmed refs: %+v", refs)
	}
	if filmPrepared(els[boy], true) || !filmPrepared(els[boy], false) {
		t.Fatal("a filmed character needs its faceless reference; other styles use the image")
	}
	p := filmReferencePrompt(&brief)
	for _, want := range []string{filmFilmedLead, "Image 1 is the character @ทหารหนุ่ม", "Image 3 is the location #แนวค่ายอยุธยา", "Image 4 is the prop ดาบไทย", "[AUDIO]", "@ทหารหนุ่ม speaks in Thai: \"ท่านขุน!\"", "No speech from @ขุนอินทร์", "footsteps on timber"} {
		if !strings.Contains(p, want) {
			t.Fatalf("reference prompt lacks %q:\n%s", want, p)
		}
	}
	if c := filmContinuePrompt(&brief); !strings.HasPrefix(c, "Image 1 is the exact first frame") || strings.Contains(c, "Image 2") {
		t.Fatalf("continue prompt must use only the first frame:\n%s", c)
	}
	brief.Style, brief.Filmed = "anime", false
	if refs = filmReferences(&brief); refs[1].Key != "film/hero.png" {
		t.Fatalf("anime refs use the character image: %+v", refs)
	}
	if p = filmReferencePrompt(&brief); strings.Contains(p, filmFilmedLead) || !strings.Contains(p, "Art style") {
		t.Fatalf("anime prompt style line:\n%s", p)
	}
}

func TestFilmShotHashTracksElementsAndCost(t *testing.T) {
	t.Parallel()
	els, hero, _, _, _ := filmTestElements()
	sh := &filmShot{Seconds: 8, Transition: "cut", CharacterIDs: []string{hero}, Action: "stands"}
	before := filmShotHash(sh, els, "anime")
	els[hero].Spec = "Thai officer, 32, blue sash"
	if filmShotHash(sh, els, "anime") == before {
		t.Fatal("changing a referenced element must make the clip stale")
	}
	if filmVideoCost(20, 8) != 32 || filmVideoCost(20, 5) != 20 || filmVideoCost(7, 4) != 6 {
		t.Fatal("video cost is the five-second price pro rata, rounded up")
	}
}

func TestFilmModelLabel(t *testing.T) {
	t.Parallel()
	for id, want := range map[string]string{"seedance-2-5": "Seedance 2.5", "seedance-2-0-fast": "Seedance 2.0 Fast", "x": "x"} {
		if got := filmModelLabel(id); got != want {
			t.Fatalf("%s: got %q want %q", id, got, want)
		}
	}
}

func TestFilmBalanceSecondsHitsTarget(t *testing.T) {
	t.Parallel()
	shots := []req.FilmShotRequest{{Seconds: 2}, {Seconds: 20}, {Seconds: 8}}
	filmBalanceSeconds(shots, 30)
	sum := 0
	for _, s := range shots {
		if s.Seconds < filmMinSeconds || s.Seconds > filmMaxSeconds {
			t.Fatalf("shot out of range: %+v", shots)
		}
		sum += s.Seconds
	}
	if sum != 30 {
		t.Fatalf("total %d, want 30: %+v", sum, shots)
	}
}

func TestFilmResolvePlanShot(t *testing.T) {
	t.Parallel()
	els, hero, boy, camp, _ := filmTestElements()
	names := map[string]string{}
	for id, el := range els {
		names[el.Kind+":"+strings.ToLower(el.Name)] = id
	}
	resolve := func(kind, name string) string { return names[kind+":"+strings.ToLower(strings.TrimSpace(name))] }
	p := filmPlanShot{Seconds: 6, Transition: "continue", Characters: []string{"ขุนอินทร์", "ไม่มีตัวนี้", "ขุนอินทร์"}, Location: "แนวค่ายอยุธยา", Action: "turns", Dialogue: []filmPlanLine{{Speaker: "ขุนอินทร์", Line: "ไปเสีย!"}, {Speaker: "ทหารหนุ่ม", Line: "not in shot"}}}
	first := filmResolveShot(p, "", true, resolve)
	if first.Transition != "cut" || len(first.CharacterIDs) != 1 || first.CharacterIDs[0] != hero || first.LocationID != camp || len(first.Dialogue) != 1 {
		t.Fatalf("first shot: %+v", first)
	}
	if next := filmResolveShot(p, camp, false, resolve); next.Transition != "continue" {
		t.Fatalf("same-location continuation must stay continue: %+v", next)
	}
	if moved := filmResolveShot(p, "elsewhere", false, resolve); moved.Transition != "cut" {
		t.Fatalf("a location change must cut: %+v", moved)
	}
	_ = boy
	if _, err := filmValidateShots([]req.FilmShotRequest{first}, nil, els, "anime"); err != nil {
		t.Fatalf("resolved shot must validate: %v", err)
	}
}

func TestFilmPlanPromptCarriesTargetAndRules(t *testing.T) {
	t.Parallel()
	full := filmPlanPrompt(&filmPlanJob{Prompt: "หนังสั้นอยุธยา", TargetSeconds: 120, Style: "cinematic", Ratio: "16:9"})
	if !strings.Contains(full, "exactly 120 seconds") || !strings.Contains(full, `"shots":[`) || !strings.Contains(full, "หนังสั้นอยุธยา") {
		t.Fatalf("full plan prompt:\n%s", full)
	}
	one := filmPlanPrompt(&filmPlanJob{Prompt: "ให้ดุขึ้น", ShotID: "x", ShotIndex: 2, Style: "anime"})
	if !strings.Contains(one, "rewrite only shot number 3") || !strings.Contains(one, `"shot":{`) {
		t.Fatalf("shot prompt:\n%s", one)
	}
}

func TestFilmFinalizableNeedsCurrentUnexpiredDraft(t *testing.T) {
	sh := &filmShot{ID: "s1", Seconds: 5}
	d := &filmDocument{Drafts: map[string]filmDraft{"s1": {TaskID: "t1", Hash: "h", Seconds: 5, CreatedAt: time.Now()}}}
	if _, ok := filmFinalizable(d, sh, "h"); !ok {
		t.Fatal("a fresh draft of the current shot should be finalizable")
	}
	if _, ok := filmFinalizable(d, sh, "edited"); ok {
		t.Fatal("a draft of an edited shot must not be finalized")
	}
	if _, ok := filmFinalizable(d, &filmShot{ID: "s1", Seconds: 8}, "h"); ok {
		t.Fatal("a draft with a different length must not be finalized")
	}
	if _, ok := filmFinalizable(d, &filmShot{ID: "s1", Seconds: 5, Video: &filmVideo{Status: "processing"}}, "h"); ok {
		t.Fatal("a busy shot must not be finalized")
	}
	d.Drafts["s1"] = filmDraft{TaskID: "t1", Hash: "h", Seconds: 5, CreatedAt: time.Now().Add(-30 * 24 * time.Hour)}
	if _, ok := filmFinalizable(d, sh, "h"); ok {
		t.Fatal("an expired draft must not be finalized")
	}
	if _, ok := filmFinalizable(&filmDocument{}, sh, "h"); ok {
		t.Fatal("a shot without a draft must not be finalized")
	}
}

func TestFilmPlanShotsKeepsShotsAndGroupsScenes(t *testing.T) {
	t.Parallel()
	els, _, _, camp, _ := filmTestElements()
	names := map[string]string{}
	for id, el := range els {
		names[el.Kind+":"+strings.ToLower(el.Name)] = id
	}
	resolve := func(kind, name string) string { return names[kind+":"+strings.ToLower(strings.TrimSpace(name))] }
	done := &filmVideo{Status: "succeeded", ClipKey: "k"}
	d := &filmDocument{Shots: []filmShot{
		{ID: "a", Seconds: 5, Transition: "cut", LocationID: camp, Action: "one", Video: done},
		{ID: "b", Seconds: 6, Transition: "cut", LocationID: camp, Action: "two", Video: done},
	}}
	plan := []filmPlanShot{
		{Keep: 1, Scene: 1},
		{Scene: 1, Seconds: 5, Location: "แนวค่ายอยุธยา", Action: "new beat"},
		{Keep: 2, Scene: 2},
		{Keep: 2, Scene: 2}, // a repeated keep is ignored
		{Keep: 9, Scene: 2}, // an unknown shot is ignored
	}
	shots, changed := filmPlanShots(plan, []string{"a", "b"}, d, resolve)
	if len(shots) != 3 || changed != 1 {
		t.Fatalf("got %d shots, %d changed: %+v", len(shots), changed, shots)
	}
	if shots[0].ID != "a" || shots[2].ID != "b" || shots[1].ID != "" {
		t.Fatalf("kept shots must keep their ids: %+v", shots)
	}
	if shots[1].Transition != "continue" || shots[2].Transition != "cut" {
		t.Fatalf("same scene continues, a new scene cuts: %+v", shots)
	}
	valid, err := filmValidateShots(shots, d.Shots, els, "anime")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if valid[0].Video == nil || valid[2].Video == nil {
		t.Fatal("kept shots must keep their videos")
	}
}

func TestFilmElementNameAndShotCount(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{"@ขุนอินทร์": "ขุนอินทร์", " #กำแพงเมือง ": "กำแพงเมือง", "ดาบ": "ดาบ"} {
		if got := filmElementName(in); got != want {
			t.Fatalf("filmElementName(%q) = %q, want %q", in, got, want)
		}
	}
	if n := filmPlanShotCount(120); n < 12 || n*15 < 120 {
		t.Fatalf("120 s needs enough shots, got %d", n)
	}
	if n := filmPlanShotCount(30); n < 3 {
		t.Fatalf("30 s got %d shots", n)
	}
}

func TestFilmSourceChangedWhenPreviousClipIsRegenerated(t *testing.T) {
	t.Parallel()
	shots := []filmShot{
		{ID: "a", Transition: "cut", Video: &filmVideo{Status: "succeeded", FrameKey: "f1"}},
		{ID: "b", Transition: "continue", Video: &filmVideo{Status: "succeeded", Mode: "continue", SourceFrameKey: "f1"}},
	}
	if filmSourceChanged(shots, 1) {
		t.Fatal("a continuation of the current previous clip is not stale")
	}
	shots[0].Video.FrameKey = "f2"
	if !filmSourceChanged(shots, 1) {
		t.Fatal("a regenerated previous clip must make the continuation stale")
	}
	if !filmSourceChanged(shots[1:], 0) {
		t.Fatal("a continuation that lost its previous shot must be stale")
	}
	if filmSourceChanged([]filmShot{{Video: &filmVideo{Status: "succeeded", FrameKey: "x"}}}, 0) {
		t.Fatal("a reference clip has no source frame")
	}
}

func TestFilmContinuePromptNamesCharactersLeftInFrame(t *testing.T) {
	t.Parallel()
	els, hero, boy, _, _ := filmTestElements()
	sh := &filmShot{ID: "s", Transition: "continue", CharacterIDs: []string{boy}, Action: "fights"}
	p := filmContinuePrompt(&filmBrief{Shot: sh, Cast: []*models.FilmElement{els[boy]}, InFrame: []*models.FilmElement{els[hero]}, Style: "anime"})
	if !strings.Contains(p, "is also in Image 1") || !strings.Contains(p, "never fights or swaps roles") {
		t.Fatalf("continue prompt must name the character left in frame:\n%s", p)
	}
	if !strings.Contains(p, "no ease-in, no stop") {
		t.Fatalf("continue prompt must keep camera speed:\n%s", p)
	}
}
