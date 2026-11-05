package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/google/uuid"
)

type directorTestFetcher struct {
	mu   sync.Mutex
	urls []string
}

func (f *directorTestFetcher) Fetch(_ context.Context, raw string) ([]byte, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.urls = append(f.urls, raw)
	return []byte("\x00\x00\x00\x18ftypmp42fixture-video"), "video/mp4", nil
}

func (d *directorFixture) expireWaits() {
	d.t.Helper()
	if e := d.db.Exec("UPDATE director_jobs SET lease_until = now() - interval '1 second' WHERE status='waiting'").Error; e != nil {
		d.t.Fatal(e)
	}
}

func (d *directorFixture) shots(p dto.DirectorProject) []dto.DirectorShot {
	out := []dto.DirectorShot{}
	for _, sc := range p.Scenes {
		out = append(out, sc.Shots...)
	}
	return out
}

func TestDirectorPostgresVideoLifecycle(t *testing.T) {
	video := modelark.NewMockClient()
	video.NextTaskID = "cgt-director-fixture-task"
	fetcher := &directorTestFetcher{}
	d := newDirectorFixtureWith(t, video, fetcher)

	status, b, _ := d.http("GET", "/capabilities", nil, nil)
	var caps struct {
		Data dto.DirectorCapabilities `json:"data"`
	}
	if status != 200 || json.Unmarshal(b, &caps) != nil || !caps.Data.VideoEnabled || len(caps.Data.Models.Video) == 0 || caps.Data.Models.Video[0].ID != "seedance-2-0-fast" || caps.Data.Models.Video[0].Name != "Seedance 2.0 Fast" {
		t.Fatalf("video capabilities: %d %s", status, b)
	}

	p := d.board()
	path := "/projects/" + p.ID
	before := d.balance()
	p = d.action(p, "/video")
	shots := d.shots(p)
	if len(shots) != 2 || shots[0].Video == nil || shots[0].Video.Status != "queued" || shots[0].Video.DurationSeconds != 5 || d.balance() != before-40 {
		t.Fatalf("video admission: %+v balance %d", shots, d.balance())
	}
	d.wantError("POST", path+"/storyboard", map[string]any{"revision": p.Revision, "idempotencyKey": uuid.NewString(), "replace": true}, 409, "DIRECTOR_STATE_CONFLICT")
	d.wantError("DELETE", path+"/shots/"+shots[0].ID, map[string]any{"revision": p.Revision}, 409, "SHOT_BUSY")

	d.tick()
	d.tick()
	if len(video.CreatedRequests) != 2 {
		t.Fatalf("submissions: %d", len(video.CreatedRequests))
	}
	r := video.CreatedRequests[0]
	if r.Model != "dreamina-seedance-2-0-fast-260128" || r.Ratio != "16:9" || r.DurationSeconds != 5 || r.Resolution != "720p" || len(r.Content) != 3 || r.Content[1].Role != modelark.RoleFirstFrame || r.Content[2].Role != modelark.RoleLastFrame || !strings.HasPrefix(r.Content[1].URL, "https://owned.test/") || r.Content[1].URL == r.Content[2].URL || !strings.Contains(r.Content[0].Text, "last frame") {
		t.Fatalf("video request: %+v", r)
	}
	if last := video.CreatedRequests[1]; len(last.Content) != 2 || last.Draft {
		t.Fatalf("last shot of a scene must use only its first frame: %+v", last)
	}
	// Waiting jobs are not resubmitted while the provider is still running.
	video.SetTask(video.NextTaskID, &modelark.TaskResult{ProviderTaskID: video.NextTaskID, Status: modelark.ProviderStatusRunning})
	d.expireWaits()
	d.tick()
	d.tick()
	if len(video.CreatedRequests) != 2 {
		t.Fatal("running task was submitted again")
	}
	p = d.get(p.ID)
	if v := d.shots(p)[0].Video; v == nil || v.Status != "processing" || v.Progress < 5 {
		t.Fatalf("processing state: %+v", v)
	}

	video.SetTask(video.NextTaskID, &modelark.TaskResult{ProviderTaskID: video.NextTaskID, Status: modelark.ProviderStatusSucceeded, VideoURL: "https://provider.test/out.mp4"})
	d.expireWaits()
	d.tick()
	d.tick()
	status, b, _ = d.http("GET", path, nil, nil)
	if status != 200 || bytes.Contains(b, []byte(video.NextTaskID)) || bytes.Contains(b, []byte("provider.test")) {
		t.Fatalf("provider details leaked: %s", b)
	}
	p = d.get(p.ID)
	for _, sh := range d.shots(p) {
		if sh.Video == nil || sh.Video.Status != "succeeded" || sh.Video.URL == nil || sh.Video.Stale {
			t.Fatalf("video not stored: %+v", sh.Video)
		}
	}
	stored := 0
	for key := range d.media.objects {
		if strings.HasSuffix(key, ".mp4") {
			stored++
		}
	}
	if stored != 2 || len(fetcher.urls) != 2 {
		t.Fatalf("stored %d fetched %d", stored, len(fetcher.urls))
	}
	d.wantError("POST", path+"/video", map[string]any{"revision": p.Revision, "idempotencyKey": uuid.NewString()}, 409, "VIDEO_UP_TO_DATE")

	edited := d.project("PATCH", path+"/shots/"+shots[0].ID, map[string]any{"revision": p.Revision, "description": "เด็กน้อยยิ้มให้ดวงดาว"})
	if v := d.shots(edited)[0].Video; v == nil || !v.Stale {
		t.Fatalf("edit did not mark video stale: %+v", v)
	}

	video.NextTaskID = "cgt-director-fixture-failed"
	before = d.balance()
	p = d.project("POST", path+"/video", map[string]any{"revision": edited.Revision, "idempotencyKey": uuid.NewString(), "shotIds": []string{shots[1].ID}})
	if d.balance() != before-20 {
		t.Fatal("single shot video not charged")
	}
	d.tick()
	video.SetTask(video.NextTaskID, &modelark.TaskResult{ProviderTaskID: video.NextTaskID, Status: modelark.ProviderStatusFailed})
	d.expireWaits()
	d.tick()
	p = d.get(p.ID)
	failed := d.shots(p)[1].Video
	if failed == nil || failed.Status != "failed" || !failed.CanRetry || failed.PointsRefunded != 20 || d.balance() != before {
		t.Fatalf("provider failure: %+v balance %d", failed, d.balance())
	}
}

func TestDirectorPostgresVideoRejectsUnsupportedRatio(t *testing.T) {
	d := newDirectorFixtureWith(t, modelark.NewMockClient(), &directorTestFetcher{})
	p := d.board()
	p = d.project("PATCH", "/projects/"+p.ID+"/storyboard-settings", map[string]any{"revision": p.Revision, "videoModelId": "seedance-2-0"})
	if p.Storyboard.VideoModelID != "seedance-2-0" {
		t.Fatal("video model not saved")
	}
	if _, e := uuid.Parse(p.ID); e != nil {
		t.Fatal(e)
	}
	if e := d.db.Exec("UPDATE director_projects SET document = jsonb_set(document, '{project,aspectRatio}', '\"4:5\"') WHERE id=?", p.ID).Error; e != nil {
		t.Fatal(e)
	}
	p = d.get(p.ID)
	d.wantError("POST", "/projects/"+p.ID+"/video", map[string]any{"revision": p.Revision, "idempotencyKey": uuid.NewString()}, 422, "MODEL_NOT_SUPPORTED")
}

func TestDirectorPostgresImagesSurviveUnusableShotPlan(t *testing.T) {
	d := newDirectorFixture(t)
	d.analyzer.reply = func(prompt string) (string, error) {
		if strings.Contains(prompt, "DIRECTOR_PLAN") {
			return "ขอโทษครับ ไม่สามารถวางแผนได้", nil
		}
		return directorPlanner(prompt)
	}
	d.fund(100)
	p := d.idea(d.create())
	p = d.action(p, "/story")
	d.tick()
	p = d.action(d.get(p.ID), "/storyboard")
	for i := 0; i < 8 && p.Storyboard.Status != "succeeded"; i++ {
		d.tick()
		p = d.get(p.ID)
	}
	if p.Storyboard.Status != "succeeded" || p.Storyboard.CompletedImages != 3 {
		t.Fatalf("images did not fall back to scene text: %+v", p.Storyboard)
	}
	for _, sh := range d.shots(p) {
		if !strings.Contains(sh.Description, "ดาวที่หายไป") {
			t.Fatalf("fallback description: %q", sh.Description)
		}
	}
}

func TestDirectorPostgresSeedance25DraftThenFinal(t *testing.T) {
	video := modelark.NewMockClient()
	video.NextTaskID = "cgt-director-draft"
	d := newDirectorFixtureWith(t, video, &directorTestFetcher{})
	p := d.board()
	path := "/projects/" + p.ID
	d.wantError("POST", path+"/video", map[string]any{"revision": p.Revision, "idempotencyKey": uuid.NewString(), "draft": true}, 422, "DRAFT_NOT_SUPPORTED")
	p = d.project("PATCH", path+"/storyboard-settings", map[string]any{"revision": p.Revision, "videoModelId": "seedance-2-5"})
	shot := d.shots(p)[0]
	before := d.balance()
	p = d.project("POST", path+"/video", map[string]any{"revision": p.Revision, "idempotencyKey": uuid.NewString(), "draft": true, "shotIds": []string{shot.ID}})
	if d.balance() != before-8 || !d.shots(p)[0].Video.Draft {
		t.Fatalf("draft admission: balance %d video %+v", d.balance(), d.shots(p)[0].Video)
	}
	d.tick()
	r := video.CreatedRequests[0]
	if r.Model != "dreamina-seedance-2-5-260628" || !r.Draft || r.Resolution != "480p" || len(r.Content) != 3 {
		t.Fatalf("draft request: %+v", r)
	}
	video.SetTask(video.NextTaskID, &modelark.TaskResult{ProviderTaskID: video.NextTaskID, Status: modelark.ProviderStatusSucceeded, VideoURL: "https://provider.test/draft.mp4"})
	d.expireWaits()
	d.tick()
	status, b, _ := d.http("GET", path, nil, nil)
	if status != 200 || bytes.Contains(b, []byte("cgt-director-draft")) {
		t.Fatalf("draft task id leaked: %s", b)
	}
	p = d.get(p.ID)
	if v := d.shots(p)[0].Video; v == nil || v.Status != "succeeded" || !v.Draft || !v.CanFinalize {
		t.Fatalf("draft result: %+v", v)
	}
	video.NextTaskID = "cgt-director-final"
	before = d.balance()
	p = d.project("POST", path+"/video", map[string]any{"revision": p.Revision, "idempotencyKey": uuid.NewString(), "finalize": true})
	if d.balance() != before-20 {
		t.Fatal("final refinement not charged at full price")
	}
	d.tick()
	final := video.CreatedRequests[1]
	if final.Model != "dreamina-seedance-2-5-260628" || final.Draft || final.Resolution != "1080p" || final.Ratio != "" || final.DurationSeconds != 0 || len(final.Content) != 1 || final.Content[0].Type != modelark.ContentTypeDraftTask || final.Content[0].DraftTaskID != "cgt-director-draft" {
		t.Fatalf("final request: %+v", final)
	}
	video.SetTask(video.NextTaskID, &modelark.TaskResult{ProviderTaskID: video.NextTaskID, Status: modelark.ProviderStatusSucceeded, VideoURL: "https://provider.test/final.mp4"})
	d.expireWaits()
	d.tick()
	p = d.get(p.ID)
	if v := d.shots(p)[0].Video; v == nil || v.Status != "succeeded" || v.Draft || v.CanFinalize {
		t.Fatalf("final result: %+v", v)
	}
}

func TestDirectorPostgresImageProgressWhileProviderRuns(t *testing.T) {
	d := newDirectorFixture(t)
	d.fund(100)
	p := d.idea(d.create())
	entered, release := d.media.block()
	p = d.action(p, "/cover")
	done := make(chan error, 1)
	go func() { done <- d.facade.Tick(context.Background()) }()
	<-entered
	deadline := time.Now().Add(6 * time.Second)
	progress := 0
	for time.Now().Before(deadline) && progress <= 5 {
		time.Sleep(500 * time.Millisecond)
		if c := d.get(p.ID).Cover; c != nil {
			progress = c.Progress
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if progress <= 5 || progress >= 100 {
		t.Fatalf("no estimated progress while waiting: %d", progress)
	}
	if c := d.get(p.ID).Cover; c == nil || c.Status != "succeeded" || c.Progress != 100 {
		t.Fatalf("cover not finished: %+v", c)
	}
}

func TestDirectorPostgresPointsBalance(t *testing.T) {
	d := newDirectorFixture(t)
	read := func() int64 {
		t.Helper()
		status, b, h := d.http("GET", "/points", nil, nil)
		var out struct {
			Data dto.DirectorPoints `json:"data"`
		}
		if status != 200 || json.Unmarshal(b, &out) != nil || h.Get("Cache-Control") != "private, no-store" {
			t.Fatalf("points: %d %s", status, b)
		}
		return out.Data.Balance
	}
	if got := read(); got != 0 {
		t.Fatalf("unfunded balance %d", got)
	}
	d.fund(100)
	d.analyzer.reply = directorPlanner
	p := d.idea(d.create())
	d.action(p, "/story")
	if got := read(); got != 96 {
		t.Fatalf("balance after story charge %d", got)
	}
	status, b, _ := d.http("GET", "/points", nil, map[string]string{"X-Test-User": uuid.NewString()})
	if status != 200 || !bytes.Contains(b, []byte(`"balance":0`)) {
		t.Fatalf("another user's balance leaked: %d %s", status, b)
	}
}

func TestDirectorPostgresSceneSettingIsEditableAndMarksImagesStale(t *testing.T) {
	d := newDirectorFixture(t)
	p := d.board()
	sc := p.Scenes[0]
	p = d.project("PATCH", "/projects/"+p.ID+"/scenes/"+sc.ID, map[string]any{"revision": p.Revision, "setting": "misty bamboo forest at dawn"})
	if p.Scenes[0].Setting != "misty bamboo forest at dawn" {
		t.Fatalf("setting not saved: %+v", p.Scenes[0])
	}
	for _, sh := range p.Scenes[0].Shots {
		if !sh.Image.Stale {
			t.Fatal("changing the scene location must mark its images stale")
		}
	}
	d.media.requests = nil
	p = d.action(p, "/shots/"+sc.Shots[0].ID+"/regenerate")
	d.tick()
	if len(d.media.requests) != 1 || !strings.HasPrefix(d.media.requests[0].Prompt, "Location (fully rendered, identical in every shot of this scene): misty bamboo forest at dawn") {
		t.Fatalf("regenerated prompt must lead with the edited location: %+v", d.media.requests)
	}
}

func TestDirectorPostgresResponsesNeverSendNullArrays(t *testing.T) {
	d := newDirectorFixture(t)
	status, b, _ := d.http("POST", "/projects", map[string]any{"name": "ชื่อซ้ำ", "aspectRatio": "16:9", "style": "cinematic", "resolution": "720p", "idempotencyKey": uuid.NewString()}, nil)
	if status != 201 || bytes.Contains(b, []byte(`"scenes":null`)) || !bytes.Contains(b, []byte(`"scenes":[]`)) {
		t.Fatalf("new project must return an empty scenes array: %d %s", status, b)
	}
	var created struct {
		Data dto.DirectorProject `json:"data"`
	}
	if err := json.Unmarshal(b, &created); err != nil {
		t.Fatal(err)
	}
	status, b, _ = d.http("GET", "/projects/"+created.Data.ID, nil, nil)
	if status != 200 || bytes.Contains(b, []byte(`"scenes":null`)) {
		t.Fatalf("get must return an empty scenes array: %d %s", status, b)
	}
	// Duplicate names are allowed; each request with a new key creates a new project.
	second := d.project("POST", "/projects", map[string]any{"name": "ชื่อซ้ำ", "aspectRatio": "16:9", "style": "cinematic", "resolution": "720p", "idempotencyKey": uuid.NewString()})
	if second.ID == created.Data.ID {
		t.Fatal("same name must not reuse the existing project")
	}
}
