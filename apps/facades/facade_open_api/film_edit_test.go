package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/google/uuid"
)

// Two scenes: ฉาก 1 = s1, s2 (continue); ฉาก 2 = s3, s4 (continue).
func filmEditShots(hero, camp string) []filmShot {
	mk := func(id, tr, action string) filmShot {
		return filmShot{ID: id, Seconds: 5, Transition: tr, CharacterIDs: []string{hero}, LocationID: camp, PropIDs: []string{}, Dialogue: []filmLine{}, Action: action}
	}
	return []filmShot{mk("s1", "cut", "a1"), mk("s2", "continue", "a2"), mk("s3", "cut", "a3"), mk("s4", "continue", "a4")}
}

func TestFilmScenesAndPositions(t *testing.T) {
	t.Parallel()
	_, hero, _, camp, _ := filmTestElements()
	scenes := filmScenes(filmEditShots(hero, camp))
	if len(scenes) != 2 || len(scenes[1]) != 2 || scenes[1][0] != 2 {
		t.Fatalf("scenes: %v", scenes)
	}
	if p := filmPosOf(scenes, 3); p != (filmPos{2, 2}) || filmPosLabel(2, 2) != "ฉาก 2 · ช็อต 2" || filmPosLabel(3, 0) != "ฉาก 3" {
		t.Fatalf("position of index 3: %+v", p)
	}
}

func TestFilmPromptCheckAsksWithoutPosition(t *testing.T) {
	t.Parallel()
	_, hero, _, camp, _ := filmTestElements()
	scenes := filmScenes(filmEditShots(hero, camp))
	ok := []struct {
		prompt string
		target *filmPos
	}{
		{"ฉาก 2 · ช็อต 1 ให้กล้องซูมเข้า", nil},
		{"เพิ่มช็อตท้ายฉาก 1 ช็อต 3", nil},
		{"ลบฉาก ๒", nil},
		{"เพิ่มฉาก 3 ตอนจบ", nil},
		{"Scene 1 shot 2: slower", nil},
		{"ให้เศร้าขึ้น", &filmPos{1, 2}},
		{"ช็อต 1 ให้มืดลง", &filmPos{2, 0}},
	}
	for _, c := range ok {
		if q := filmPromptCheck(c.prompt, scenes, c.target); q != "" {
			t.Fatalf("%q should pass, asked %q", c.prompt, q)
		}
	}
	for _, p := range []string{"ฉาก 1 · ช็อต 2 และฉาก 2 ให้มืดลง", "ฉาก 1 ช็อต 2, ฉาก 2 ช็อต 1"} {
		if q := filmPromptCheck(p, scenes, nil); q != "" {
			t.Fatalf("%q lists positions, asked %q", p, q)
		}
	}
	if q := filmPromptCheck("ช็อต 2 ของฉาก 2 ให้ช้าลง", scenes, &filmPos{1, 0}); q != "" {
		t.Fatalf("shot-then-scene order must pass, asked %q", q)
	}
	ask := []string{"ทำให้ทั้งเรื่องเศร้าขึ้น", "ฉาก 4 ให้ฝนตก", "ฉาก 1 · ช็อต 5", "ช็อต 2 ให้ช้าลง", "ฉาก 99999999999999999999", "take a screenshot 9", "ช็อต 4 ของฉาก 2"}
	for _, p := range ask {
		if q := filmPromptCheck(p, scenes, nil); q == "" || !strings.Contains(q, "ไม่คิดแต้ม") {
			t.Fatalf("%q should ask back, got %q", p, q)
		}
	}
}

func filmEditResolver(byID map[string]*models.FilmElement) func(kind, name string) string {
	return func(kind, name string) string {
		for id, el := range byID {
			if el.Kind == kind && el.Name == filmElementName(name) {
				return id
			}
		}
		return ""
	}
}

func TestFilmResolveAndApplyOps(t *testing.T) {
	t.Parallel()
	els, hero, _, camp, _ := filmTestElements()
	d := filmDocument{Shots: filmEditShots(hero, camp)}
	resolve := filmEditResolver(els)
	shot := func(action string) *filmPlanShot {
		return &filmPlanShot{Seconds: 6, Characters: []string{"ขุนอินทร์"}, Location: "แนวค่ายอยุธยา", Action: action}
	}
	ops, err := filmResolveOps([]filmPlanOp{
		{Op: "edit", Scene: 1, Shot: 2, Data: shot("edited")},
		{Op: "add", Scene: 1, Shot: 3, Data: shot("tail of scene 1")},
		{Op: "delete", Scene: 2, Shot: 1},
		{Op: "add", Scene: 3, NewScene: true, Data: shot("new scene A")},
		{Op: "add", Scene: 3, NewScene: true, Data: shot("new scene B")},
	}, &d, resolve, els)
	if err != nil {
		t.Fatal(err)
	}
	shots, err := filmApplyOps(d.Shots, ops)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, s := range shots {
		got = append(got, s.Action+"/"+s.Transition)
	}
	want := "a1/cut,edited/continue,tail of scene 1/continue,a4/cut,new scene A/cut,new scene B/continue"
	if strings.Join(got, ",") != want {
		t.Fatalf("applied:\n got %s\nwant %s", strings.Join(got, ","), want)
	}
	if shots[1].ID != "s2" || shots[2].ID != "" || shots[3].ID != "s4" {
		t.Fatalf("ids: edited shot keeps its id, new shots have none: %+v", shots)
	}
	if _, err = filmValidateShots(shots, d.Shots, els, "anime"); err != nil {
		t.Fatalf("result must validate: %v", err)
	}

	// Insert in front of a scene's first shot: the new shot opens the scene, the old one continues.
	ops, _ = filmResolveOps([]filmPlanOp{{Op: "add", Scene: 2, Shot: 1, Data: shot("opener")}}, &d, resolve, els)
	shots, _ = filmApplyOps(d.Shots, ops)
	if shots[2].Action != "opener" || shots[2].Transition != "cut" || shots[3].ID != "s3" || shots[3].Transition != "continue" {
		t.Fatalf("lead insert: %+v", shots)
	}

	// Positions that do not exist are rejected with a question.
	var fe *filmEditError
	for _, bad := range []filmPlanOp{{Op: "edit", Scene: 1, Shot: 3, Data: shot("x")}, {Op: "delete", Scene: 5}, {Op: "add", Scene: 1, Shot: 4, Data: shot("x")}, {Op: "voice", Character: "ไม่มีตัวนี้", Voice: "nara"}} {
		if _, err = filmResolveOps([]filmPlanOp{bad}, &d, resolve, els); !errors.As(err, &fe) {
			t.Fatalf("%+v must be rejected, got %v", bad, err)
		}
	}

	// Deleting every shot is refused.
	ops, _ = filmResolveOps([]filmPlanOp{{Op: "delete", Scene: 1}, {Op: "delete", Scene: 2}}, &d, resolve, els)
	if _, err = filmApplyOps(d.Shots, ops); !errors.As(err, &fe) {
		t.Fatalf("deleting all shots: %v", err)
	}
}

func (s *filmFakeStore) CompletePlan(_ context.Context, j *models.FilmJob, fn func(*models.FilmEpisode, []models.FilmElement, *models.DirectorSeries) ([]models.FilmElement, error)) error {
	created, e := fn(&s.episode, s.elements, &s.series)
	if e != nil {
		return e
	}
	s.finished[j.ID] = "succeeded"
	s.elements = append(s.elements, created...)
	return nil
}

func (s *filmFakeStore) ChangeElement(_ context.Context, id, _, _, _ string, fn func(*models.FilmElement, *models.DirectorSeries, repo.FilmAssetLoader) ([]models.FilmJob, error)) (*models.FilmElement, error) {
	for i := range s.elements {
		if s.elements[i].ID.String() == id {
			_, e := fn(&s.elements[i], &s.series, nil)
			return &s.elements[i], e
		}
	}
	return nil, ErrFilmNotFound
}

type filmFakeAnalyzer struct{ answer, prompt string }

func (a *filmFakeAnalyzer) Analyze(_ context.Context, _ string, prompt string) (string, error) {
	a.prompt = prompt
	return a.answer, nil
}

func filmEditFixture(t *testing.T, answer string) (*FilmFacade, *filmFakeStore, string) {
	t.Helper()
	els, hero, _, camp, _ := filmTestElements()
	store := &filmFakeStore{finished: map[uuid.UUID]string{}, refunded: map[uuid.UUID]bool{}}
	for _, el := range els {
		store.elements = append(store.elements, *el)
	}
	shots := filmEditShots(hero, camp)
	shots[2].Video = &filmVideo{Status: "succeeded", Hash: "h", ClipKey: "c3"}
	raw, _ := json.Marshal(filmDocument{Shots: shots})
	store.project.ID, store.series.ID, store.series.Style, store.series.AspectRatio = uuid.New(), uuid.New(), "anime", "16:9"
	store.episode = models.FilmEpisode{ProjectID: store.project.ID, Document: raw}
	f := NewFilmFacade(store, &filmFakeMedia{uploads: map[string][]byte{}}, nil, nil, nil, FilmOptions{Analyzer: &filmFakeAnalyzer{answer: answer}, AnalysisModel: "m", PlanPoints: 8})
	return f, store, store.project.ID.String()
}

func (s *filmFakeStore) doc(t *testing.T) filmDocument {
	t.Helper()
	d, e := filmDecode(&s.episode)
	if e != nil {
		t.Fatal(e)
	}
	return d
}

func TestFilmPlanWithoutPositionAsksForFree(t *testing.T) {
	f, store, project := filmEditFixture(t, "")
	out, e := f.RequestPlan(context.Background(), project, uuid.NewString(), req.FilmPlanRequest{IdempotencyKey: uuid.NewString(), Prompt: "ทำให้ทั้งเรื่องเศร้าขึ้น"})
	if e != nil {
		t.Fatal(e)
	}
	if len(store.jobs) != 0 || out.Plan != nil || len(out.Messages) != 2 || out.Messages[1].Role != "assistant" {
		t.Fatalf("must ask back without a job: jobs=%d msgs=%+v", len(store.jobs), out.Messages)
	}
	// With a selected shot the request is queued at a quarter of the price.
	sel := store.doc(t).Shots[1].ID
	if _, e = f.RequestPlan(context.Background(), project, uuid.NewString(), req.FilmPlanRequest{IdempotencyKey: uuid.NewString(), Prompt: "ให้เศร้าขึ้น", ShotID: sel}); e != nil {
		t.Fatal(e)
	}
	if len(store.jobs) != 1 || store.jobs[0].Cost != 2 {
		t.Fatalf("one quarter-price job: %+v", store.jobs)
	}
	var snap filmPlanJob
	_ = json.Unmarshal(store.jobs[0].Snapshot, &snap)
	if snap.Mode != filmPlanModeOps || snap.Target != "ฉาก 1 · ช็อต 2" || snap.Shots[3].Scene != 2 || snap.Shots[3].InScene != 2 {
		t.Fatalf("snapshot: %+v", snap)
	}
	if p := filmOpsPrompt(&snap); !strings.Contains(p, "ฉาก 1 · ช็อต 2 in the timeline") || !strings.Contains(p, `"op":"edit"`) {
		t.Fatal("ops prompt must carry the target and the operation schema")
	}
}

func runPlanJob(t *testing.T, f *FilmFacade, store *filmFakeStore, project, prompt string) {
	t.Helper()
	if _, e := f.RequestPlan(context.Background(), project, uuid.NewString(), req.FilmPlanRequest{IdempotencyKey: uuid.NewString(), Prompt: prompt}); e != nil {
		t.Fatal(e)
	}
	j := store.jobs[len(store.jobs)-1]
	j.UserID = uuid.New()
	if e := f.planJob(context.Background(), &j); e != nil {
		t.Fatal(e)
	}
}

func TestFilmOpsPlanAppliesOrWaitsForConfirmation(t *testing.T) {
	// Editing ฉาก 1 · ช็อต 2 applies at once.
	f, store, project := filmEditFixture(t, `{"reply":"แก้แล้ว","ops":[{"op":"edit","scene":1,"shot":2,"data":{"seconds":7,"characters":["ขุนอินทร์"],"location":"แนวค่ายอยุธยา","action":"looks back"}}]}`)
	runPlanJob(t, f, store, project, "ฉาก 1 · ช็อต 2 ให้หันกลับ")
	d := store.doc(t)
	if d.Plan != nil || d.Pending != nil || d.Shots[1].Action != "looks back" || d.Shots[1].ID != "s2" || d.Shots[1].Transition != "continue" {
		t.Fatalf("edit applied: %+v pending=%+v", d.Shots[1], d.Pending)
	}

	// Deleting ฉาก 2 · ช็อต 1 (it has a paid clip) waits for the card.
	f, store, project = filmEditFixture(t, `{"reply":"ลบแล้ว","ops":[{"op":"delete","scene":2,"shot":1}]}`)
	runPlanJob(t, f, store, project, "ลบฉาก 2 · ช็อต 1")
	d = store.doc(t)
	if d.Pending == nil || len(d.Shots) != 4 || len(d.Pending.Deletes) != 1 || d.Pending.Deletes[0] != "ฉาก 2 · ช็อต 1" {
		t.Fatalf("delete must wait: shots=%d pending=%+v", len(d.Shots), d.Pending)
	}
	out, e := f.ConfirmPlan(context.Background(), project, uuid.NewString(), req.FilmPlanConfirmRequest{IdempotencyKey: uuid.NewString(), PendingID: d.Pending.ID, Accept: true})
	if e != nil {
		t.Fatal(e)
	}
	if len(out.Shots) != 3 || out.Shots[2].ID != "s4" || out.Shots[2].Transition != "cut" || out.Pending != nil {
		t.Fatalf("confirmed delete: %+v", out.Shots)
	}
	var de *DirectorError
	if _, e = f.ConfirmPlan(context.Background(), project, uuid.NewString(), req.FilmPlanConfirmRequest{IdempotencyKey: uuid.NewString(), PendingID: d.Pending.ID, Accept: true}); !errors.As(e, &de) || de.Code != "PLAN_STALE" {
		t.Fatalf("second confirm: %v", e)
	}

	// A position the assistant invents is asked back and refunded.
	f, store, project = filmEditFixture(t, `{"reply":"","ops":[{"op":"delete","scene":7,"shot":1}]}`)
	runPlanJob(t, f, store, project, "ลบฉาก 2")
	j := store.jobs[len(store.jobs)-1]
	if !store.refunded[j.ID] || store.doc(t).Plan != nil || len(store.doc(t).Shots) != 4 {
		t.Fatalf("invalid position must be refunded and change nothing")
	}

	// A question from the assistant is shown and refunded.
	f, store, project = filmEditFixture(t, `{"reply":"","question":"ต้องการให้เศร้าขึ้นตรงช็อตไหน?","ops":[]}`)
	runPlanJob(t, f, store, project, "ฉาก 1 เศร้าขึ้น")
	d = store.doc(t)
	if last := d.Messages[len(d.Messages)-1]; !strings.Contains(last.Text, "ช็อตไหน") || !store.refunded[store.jobs[0].ID] {
		t.Fatalf("question: %+v", last)
	}
}

func TestFilmVoiceOpWaitsForPricedConfirmation(t *testing.T) {
	f, store, project := filmEditFixture(t, `{"reply":"เปลี่ยนเสียงแล้ว","ops":[{"op":"voice","character":"ขุนอินทร์","voice":"krit"}]}`)
	f.options.VideoPoints = 10
	// ฉาก 2 · ช็อต 1: hero speaks; its clip was voiced by the provider (legacy) → a new video.
	d := store.doc(t)
	var hero *models.FilmElement
	for i := range store.elements {
		if store.elements[i].Name == "ขุนอินทร์" {
			hero = &store.elements[i]
		}
	}
	hero.Voice = filmPlanVoice("nara")
	byID := filmElementMap(store.elements)
	d.Shots[2].Dialogue = []filmLine{{SpeakerID: hero.ID.String(), Line: "ถอยไม่ได้แล้ว"}}
	d.Shots[2].Video = &filmVideo{Status: "succeeded", Hash: filmShotHash(&d.Shots[2], byID, "anime"), ClipKey: "c3", VoiceHash: filmNoVoices}
	d.Shots[3].Video = &filmVideo{Status: "succeeded", Hash: filmShotHash(&d.Shots[3], byID, "anime"), ClipKey: "c4"}
	raw, _ := json.Marshal(d)
	store.episode.Document = raw
	runPlanJob(t, f, store, project, "ฉาก 2 · ช็อต 1 เปลี่ยนเสียงขุนอินทร์ให้ทุ้มขึ้น")
	p := store.doc(t).Pending
	if p == nil || len(p.Voices) != 1 || p.Voices[0].RemakeClips != 1 || p.VideoPoints != 11 || p.Stale != 1 {
		t.Fatalf("voice change must wait with its price: %+v", p)
	}
	if v := filmBoundVoice(hero); v.VoiceID != "nara" {
		t.Fatal("voice must not change before confirmation")
	}
}

func TestFilmApplyOpsSceneRules(t *testing.T) {
	t.Parallel()
	els, hero, _, camp, _ := filmTestElements()
	d := filmDocument{Shots: filmEditShots(hero, camp)}
	resolve := filmEditResolver(els)
	shot := func(action string) *filmPlanShot {
		return &filmPlanShot{Seconds: 6, Characters: []string{"ขุนอินทร์"}, Location: "แนวค่ายอยุธยา", Action: action}
	}
	apply := func(ops ...filmPlanOp) string {
		t.Helper()
		resolved, err := filmResolveOps(ops, &d, resolve, els)
		if err != nil {
			t.Fatal(err)
		}
		shots, err := filmApplyOps(d.Shots, resolved)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, s := range shots {
			got = append(got, s.Action+"/"+s.Transition)
		}
		return strings.Join(got, ",")
	}
	// Deleting the scene's first shot and adding as its new 2nd shot keeps the new shot in scene 2.
	if got := apply(filmPlanOp{Op: "delete", Scene: 2, Shot: 1}, filmPlanOp{Op: "add", Scene: 2, Shot: 2, Data: shot("n")}); got != "a1/cut,a2/continue,n/cut,a4/continue" {
		t.Fatalf("delete+add: %s", got)
	}
	// Adds in front of one shot: end of scene 1 first, then the opener of scene 2, whatever the op order.
	if got := apply(filmPlanOp{Op: "add", Scene: 2, Shot: 1, Data: shot("open2")}, filmPlanOp{Op: "add", Scene: 1, Shot: 3, Data: shot("end1")}); got != "a1/cut,a2/continue,end1/continue,open2/cut,a3/continue,a4/continue" {
		t.Fatalf("order: %s", got)
	}
	// A whole scene deleted and a shot appended to it starts a scene of its own.
	if got := apply(filmPlanOp{Op: "delete", Scene: 1}, filmPlanOp{Op: "add", Scene: 1, Shot: 3, Data: shot("x")}); got != "x/cut,a3/cut,a4/continue" {
		t.Fatalf("deleted scene: %s", got)
	}
	// A lead insert in another place leaves the old first shot opening its own scene.
	other := *shot("elsewhere")
	other.Location = ""
	if got := apply(filmPlanOp{Op: "add", Scene: 2, Shot: 1, Data: &other}); got != "a1/cut,a2/continue,elsewhere/cut,a3/cut,a4/continue" {
		t.Fatalf("lead elsewhere: %s", got)
	}
	// A new shot elsewhere in the middle of a scene: the stored shot after it cannot continue from it.
	if got := apply(filmPlanOp{Op: "add", Scene: 2, Shot: 2, Data: &other}); got != "a1/cut,a2/continue,a3/cut,elsewhere/cut,a4/cut" {
		t.Fatalf("middle elsewhere: %s", got)
	}
	// A partial edit keeps what it does not mention; seconds are clamped.
	resolved, _ := filmResolveOps([]filmPlanOp{{Op: "edit", Scene: 1, Shot: 1, Data: &filmPlanShot{Seconds: 40, Camera: "slow push in"}}}, &d, resolve, els)
	e := resolved[0].Shot
	if e.Action != "a1" || e.LocationID != camp || len(e.CharacterIDs) != 1 || e.Camera != "slow push in" || e.Seconds != filmMaxSeconds {
		t.Fatalf("partial edit: %+v", e)
	}
}

func TestFilmOpsJobSendsOpsPrompt(t *testing.T) {
	f, store, project := filmEditFixture(t, `{"reply":"ok","ops":[{"op":"edit","scene":1,"shot":1,"data":{"camera":"wide"}}]}`)
	runPlanJob(t, f, store, project, "ฉาก 1 · ช็อต 1 กล้องกว้างขึ้น")
	a := f.options.Analyzer.(*filmFakeAnalyzer)
	if !strings.Contains(a.prompt, `"op":"edit"`) || strings.Contains(a.prompt, `"keep"`) {
		t.Fatal("an edit job must ask for operations, not a shot list")
	}
	if d := store.doc(t); d.Shots[0].Camera != "wide" || d.Shots[0].Action != "a1" {
		t.Fatalf("edit applied over the current shot: %+v", d.Shots[0])
	}
}

func TestFilmVoiceImpactCountsClipsWithVoicesInTheirHash(t *testing.T) {
	f, store, _ := filmEditFixture(t, "")
	f.options.VideoPoints = 10
	byID := filmElementMap(store.elements)
	var hero *models.FilmElement
	for _, el := range byID {
		if el.Name == "ขุนอินทร์" {
			hero = el
		}
	}
	hero.Voice = filmPlanVoice("nara")
	sh := filmEditShots(hero.ID.String(), "")[0]
	sh.LocationID = ""
	sh.Dialogue = []filmLine{{SpeakerID: hero.ID.String(), Line: "ถอยไม่ได้"}}
	base := filmShotHash(&sh, byID, "anime")
	// A clip from before voices were kept apart: its hash carries today's voices.
	sh.Video = &filmVideo{Status: "succeeded", Hash: filmVoiceHash(base, filmVoiceLines(&sh, byID)), ClipKey: "c"}
	d := filmDocument{Shots: []filmShot{sh}}
	ops := []filmEditOp{{Op: "voice", Voice: &filmEditVoiceTarget{CharacterID: hero.ID.String(), Voice: filmPlanVoice("krit")}}}
	after, changed := filmVoicesAfter(byID, ops)
	_, _, points, remake, _ := f.filmVoiceImpact(&d, byID, after, "anime", changed)
	if len(remake) != 1 || points != 11 {
		t.Fatalf("legacy clip must be re-made: remake=%v points=%d", remake, points)
	}
}

func TestFilmConfirmRefusedAfterLayoutChanges(t *testing.T) {
	f, store, project := filmEditFixture(t, `{"reply":"ลบแล้ว","ops":[{"op":"delete","scene":2,"shot":1}]}`)
	runPlanJob(t, f, store, project, "ลบฉาก 2 · ช็อต 1")
	d := store.doc(t)
	if d.Pending == nil {
		t.Fatal("pending expected")
	}
	// Someone moves the shots by hand (without clearing the pending edit).
	d.Shots[0], d.Shots[1] = d.Shots[1], d.Shots[0]
	d.Shots[0].Transition, d.Shots[1].Transition = "cut", "continue"
	raw, _ := json.Marshal(d)
	store.episode.Document = raw
	var de *DirectorError
	if _, e := f.ConfirmPlan(context.Background(), project, uuid.NewString(), req.FilmPlanConfirmRequest{IdempotencyKey: uuid.NewString(), PendingID: d.Pending.ID, Accept: true}); !errors.As(e, &de) || de.Code != "PLAN_STALE" {
		t.Fatalf("confirm on a changed layout: %v", e)
	}
}

func TestFilmCinemaLookInFilmedPrompts(t *testing.T) {
	t.Parallel()
	els, hero, _, camp, _ := filmTestElements()
	sh := filmShot{ID: "s", Seconds: 5, Transition: "cut", CharacterIDs: []string{hero}, LocationID: camp, Action: "sits", Dialogue: []filmLine{}}
	b := &filmBrief{Shot: &sh, Cast: []*models.FilmElement{els[hero]}, Location: els[camp], Style: "realistic", Filmed: true}
	p := filmReferencePrompt(b)
	for _, want := range []string{"shallow depth of field", "35mm film grain", "do not copy their pose", "Avoid: centered symmetrical framing"} {
		if !strings.Contains(p, want) {
			t.Fatalf("filmed prompt lacks %q", want)
		}
	}
	b.Filmed, b.Style = false, "anime"
	if strings.Contains(filmReferencePrompt(b), "Avoid: centered") {
		t.Fatal("stylized prompts keep their own look")
	}
	if !strings.Contains(filmPlanRulesText(true), "never into the lens") || strings.Contains(filmPlanRulesText(false), "never into the lens") {
		t.Fatal("cinematography rules only for filmed plans")
	}
	if loc := filmElementPrompt(filmElementJob{Kind: "location", Name: "x", Spec: "office", Style: "realistic", Filmed: true}); !strings.Contains(loc, "film still") || strings.Contains(loc, "Establishing reference plate") {
		t.Fatalf("filmed location plate: %s", loc)
	}
}

func TestFilmRoomToneLevel(t *testing.T) {
	t.Parallel()
	wav := filmRoomTone(2, filmRoomToneDb)
	if string(wav[:4]) != "RIFF" || len(wav) != 44+2*(2*44100+1) {
		t.Fatalf("wav header/size: %d", len(wav))
	}
	sum, n := 0.0, (len(wav)-44)/2
	for i := 0; i < n; i++ {
		v := float64(int16(uint16(wav[44+2*i])|uint16(wav[45+2*i])<<8)) / 32767
		sum += v * v
	}
	if db := 10 * math.Log10(sum/float64(n)); math.Abs(db-filmRoomToneDb) > 1.5 {
		t.Fatalf("room tone at %.1f dBFS, want about %.0f", db, filmRoomToneDb)
	}
}

func TestFilmVoiceAnchorIsStoredPerCharacterAndIgnoresMood(t *testing.T) {
	media := &filmFakeMedia{uploads: map[string][]byte{}}
	f := NewFilmFacade(&filmFakeStore{}, media, nil, nil, nil, FilmOptions{})
	voices := &filmFakeVoices{}
	f.voices = voices
	v := models.CharacterVoice{VoiceID: "krit", Kind: "preset", Emotion: "โกรธ", Speed: 1}
	if _, e := f.voiceAnchor(context.Background(), "u", "hero", v); e != nil {
		t.Fatal(e)
	}
	if len(media.uploads) != 1 || len(voices.spoken) != 1 || !strings.HasPrefix(voices.spoken[0], "krit:"+filmVoiceAnchorText[:9]) {
		t.Fatalf("anchor generated once and stored: %v %v", media.uploads, voices.spoken)
	}
	calm := v
	calm.Emotion = "ปกติ"
	if filmVoiceIdentity(v) != filmVoiceIdentity(calm) {
		t.Fatal("the anchor does not depend on the line's mood")
	}
	other := v
	other.VoiceID = "nara"
	if filmVoiceIdentity(v) == filmVoiceIdentity(other) {
		t.Fatal("another voice gets another anchor")
	}
}

// filmTimedVoices speaks every line as a WAV of base/speed seconds.
type filmTimedVoices struct {
	filmFakeVoices
	base   float64
	speeds []float64
}

func (v *filmTimedVoices) SpeakLine(_ context.Context, _ string, cv models.CharacterVoice, _, _ string) ([]byte, error) {
	return filmRoomTone(2, -20), nil
}
func (v *filmTimedVoices) SpeakLineAs(_ context.Context, _ string, cv models.CharacterVoice, _, note string, _ []byte) ([]byte, error) {
	s := cv.Speed
	if s <= 0 {
		s = 1
	}
	v.speeds = append(v.speeds, s)
	if strings.Contains(note, "brisk") {
		return filmRoomTone(v.base*0.5, -20), nil
	}
	return filmRoomTone(v.base, -20), nil
}

func TestFilmLinesArePlayedFasterToFitTheShot(t *testing.T) {
	media := &filmFakeMedia{uploads: map[string][]byte{}}
	f := NewFilmFacade(&filmFakeStore{}, media, nil, nil, nil, FilmOptions{})
	voices := &filmTimedVoices{base: 6.6}
	f.voices = voices
	lines := []filmVoiceLine{{SpeakerID: "a", Text: "x", Voice: models.CharacterVoice{VoiceID: "nara", Kind: "preset", Speed: 1}}}
	out, e := f.speakLines(context.Background(), "u", "s", lines, t.TempDir(), 6)
	if e != nil {
		t.Fatal(e)
	}
	end := out[0].At + out[0].Seconds
	if len(voices.speeds) != 1 || end > 5.75 || out[0].Seconds > 5.6 {
		t.Fatalf("a 6.6 s line must be sped up (once spoken) to end before 5.7 s: calls=%d end=%.2f", len(voices.speeds), end)
	}
	// A line that fits keeps its pace and lead.
	voices.base, voices.speeds = 3, nil
	out, e = f.speakLines(context.Background(), "u", "s", lines, t.TempDir(), 6)
	if e != nil || out[0].At != filmDubLead || out[0].Seconds < 2.9 {
		t.Fatalf("fitting line changed: %+v %v", out[0].filmDubLine, e)
	}
}

// filmFlakyAnalyzer answers badly once, then well.
type filmFlakyAnalyzer struct {
	answers []string
	calls   int
}

func (a *filmFlakyAnalyzer) Analyze(context.Context, string, string) (string, error) {
	a.calls++
	return a.answers[min(a.calls-1, len(a.answers)-1)], nil
}

func TestFilmPlanRetriesAnUnreadableAnswerOnce(t *testing.T) {
	f, store, project := filmEditFixture(t, "")
	a := &filmFlakyAnalyzer{answers: []string{`{"reply":"bad "quote" here","ops":[]}`, `{"reply":"ok","ops":[{"op":"edit","scene":1,"shot":1,"data":{"camera":"wide"}}]}`}}
	f.options.Analyzer = a
	runPlanJob(t, f, store, project, "ฉาก 1 · ช็อต 1 กล้องกว้าง")
	if a.calls != 2 || store.doc(t).Shots[0].Camera != "wide" {
		t.Fatalf("retry once and apply: calls=%d camera=%q", a.calls, store.doc(t).Shots[0].Camera)
	}
}

func TestFilmFilmedPromptLeadsWithTheCamera(t *testing.T) {
	t.Parallel()
	els, hero, _, camp, _ := filmTestElements()
	sh := filmShot{ID: "s", Seconds: 5, Transition: "cut", CharacterIDs: []string{hero}, LocationID: camp, Action: "sits", Camera: "Profile medium shot, 50mm.", Dialogue: []filmLine{}}
	p := filmReferencePrompt(&filmBrief{Shot: &sh, Cast: []*models.FilmElement{els[hero]}, Location: els[camp], Style: "realistic", Filmed: true})
	if !strings.HasPrefix(p, "SHOT (follow exactly): Profile medium shot, 50mm.") || strings.Count(p, "Profile medium shot") != 1 {
		t.Fatalf("camera must lead once: %.200s", p)
	}
}

func TestFilmStaticCameraIsLockedInBothPromptStyles(t *testing.T) {
	t.Parallel()
	els, hero, _, camp, _ := filmTestElements()
	for _, filmed := range []bool{true, false} {
		sh := filmShot{ID: "s", Seconds: 5, Transition: "cut", CharacterIDs: []string{hero}, LocationID: camp, Action: "sits", Camera: "Static locked-off medium shot, 50mm.", Dialogue: []filmLine{}}
		style := "anime"
		if filmed {
			style = "realistic"
		}
		p := filmReferencePrompt(&filmBrief{Shot: &sh, Cast: []*models.FilmElement{els[hero]}, Location: els[camp], Style: style, Filmed: filmed})
		if strings.Count(p, filmLockedCamera) != 1 {
			t.Fatalf("filmed=%t: a static shot is locked once: %.300s", filmed, p)
		}
		sh.Camera = "Medium shot, 50mm, slow push in."
		if p = filmReferencePrompt(&filmBrief{Shot: &sh, Cast: []*models.FilmElement{els[hero]}, Location: els[camp], Style: style, Filmed: filmed}); strings.Contains(p, filmLockedCamera) {
			t.Fatalf("filmed=%t: a moving shot is not locked", filmed)
		}
	}
	for camera, want := range map[string]bool{"STATIC wide": true, "Locked off close-up": true, "on a tripod, 85mm": true, "tracking shot": false, "": false} {
		if filmStaticCamera(camera) != want {
			t.Errorf("filmStaticCamera(%q) != %t", camera, want)
		}
	}
}

func TestFilmDubWithoutAnyLineIsNotShipped(t *testing.T) {
	f := NewFilmFacade(&filmFakeStore{}, &filmFakeMedia{uploads: map[string][]byte{}}, nil, nil, nil, FilmOptions{Processor: &filmFakeMixer{}})
	f.voices = &filmSkipVoices{}
	in := &filmVideoJob{ShotID: "s", Seconds: 6, Voices: []filmVoiceLine{{SpeakerID: "a", Text: "x", Voice: models.CharacterVoice{VoiceID: "gone"}}}}
	if _, _, e := f.dubClip(context.Background(), "u", in, []byte("clip")); !errors.Is(e, errFilmDubRetry) {
		t.Fatalf("a dubbed shot with no spoken line must not keep the provider voice: %v", e)
	}
}

type filmSkipVoices struct{ filmFakeVoices }

func (v *filmSkipVoices) SpeakLine(context.Context, string, models.CharacterVoice, string, string) ([]byte, error) {
	return nil, ErrValidation
}
func (v *filmSkipVoices) SpeakLineAs(context.Context, string, models.CharacterVoice, string, string, []byte) ([]byte, error) {
	return nil, ErrValidation
}

func TestFilmTooSlowLinesGetAQuickerTake(t *testing.T) {
	f := NewFilmFacade(&filmFakeStore{}, &filmFakeMedia{uploads: map[string][]byte{}}, nil, nil, nil, FilmOptions{})
	voices := &filmTimedVoices{base: 10}
	f.voices = voices
	lines := []filmVoiceLine{{SpeakerID: "a", Text: "x", Voice: models.CharacterVoice{VoiceID: "tawan", Kind: "preset", Speed: 1}}}
	out, e := f.speakLines(context.Background(), "u", "s", lines, t.TempDir(), 6)
	if e != nil {
		t.Fatal(e)
	}
	if end := out[0].At + out[0].Seconds; len(voices.speeds) != 2 || end > 5.75 {
		t.Fatalf("a 10 s take must be re-spoken briskly and fit: calls=%d end=%.2f", len(voices.speeds), end)
	}
}
