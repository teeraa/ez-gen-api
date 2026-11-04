package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

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
	if status != 200 || json.Unmarshal(b, &caps) != nil || !caps.Data.VideoEnabled || len(caps.Data.Models.Video) == 0 || caps.Data.Models.Video[0].ID != "violet-motion-2" || caps.Data.Models.Video[0].Name != "Seedance 2.0 Fast" {
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
	if r.Model != "dreamina-seedance-2-0-fast-260128" || r.Ratio != "16:9" || r.DurationSeconds != 5 || r.Resolution != "720p" || len(r.Content) != 2 || r.Content[1].Role != modelark.RoleFirstFrame || !strings.HasPrefix(r.Content[1].URL, "https://owned.test/") {
		t.Fatalf("video request: %+v", r)
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
	p = d.project("PATCH", "/projects/"+p.ID+"/storyboard-settings", map[string]any{"revision": p.Revision, "videoModelId": "violet-classic"})
	if p.Storyboard.VideoModelID != "violet-classic" {
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
