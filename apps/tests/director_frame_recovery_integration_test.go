package tests

import (
	"context"
	"encoding/json"
	"fmt"
	facade "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/google/uuid"
)

func legacyDirectorFrameJob(t *testing.T) (*directorFixture, models.DirectorJob) {
	t.Helper()
	d := newDirectorFixtureWith(t, modelark.NewMockClient(), &directorTestFetcher{})
	p := d.board()
	shot := d.shots(p)[0]
	p = d.project("POST", "/projects/"+p.ID+"/video", map[string]any{"revision": p.Revision, "idempotencyKey": uuid.NewString(), "shotIds": []string{shot.ID}})
	var job models.DirectorJob
	if e := d.db.Where("project_id=? AND kind='video' AND status='queued'", p.ID).First(&job).Error; e != nil {
		t.Fatal(e)
	}
	var row models.DirectorProject
	if e := d.db.First(&row, "id=?", p.ID).Error; e != nil {
		t.Fatal(e)
	}
	var doc map[string]json.RawMessage
	if e := json.Unmarshal(row.Document, &doc); e != nil {
		t.Fatal(e)
	}
	for i := range p.Scenes {
		for k := range p.Scenes[i].Shots {
			sh := &p.Scenes[i].Shots[k]
			if sh.ID == shot.ID {
				code := "VIDEO_LAST_FRAME_MISSING"
				sh.Video.Status = "needs_attention"
				sh.Video.DirectorFailure = dto.DirectorFailure{FailureCode: &code}
			}
		}
	}
	doc["project"], _ = json.Marshal(p)
	encoded, _ := json.Marshal(doc)
	if e := d.db.Model(&row).Update("document", encoded).Error; e != nil {
		t.Fatal(e)
	}
	if e := d.db.Model(&job).Updates(map[string]any{"status": "needs_attention", "output": nil}).Error; e != nil {
		t.Fatal(e)
	}
	job.Status = "needs_attention"
	job.Output = nil
	return d, job
}
func TestDirectorPostgresFrameRecoveryClaimAtomicAndBillingUnchanged(t *testing.T) {
	d, original := legacyDirectorFrameJob(t)
	before := d.balance()
	var entries int64
	d.db.Model(&models.DirectorPointEntry{}).Count(&entries)
	var wg sync.WaitGroup
	claims := make(chan *models.DirectorJob, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			j, e := d.store.Claim(context.Background())
			if e == nil {
				claims <- j
			}
		}()
	}
	wg.Wait()
	close(claims)
	got := []*models.DirectorJob{}
	for j := range claims {
		got = append(got, j)
	}
	if len(got) != 1 || got[0].ID != original.ID {
		t.Fatalf("legacy recovery leased %d times", len(got))
	}
	var persisted models.DirectorJob
	if e := d.db.First(&persisted, "id=?", original.ID).Error; e != nil {
		t.Fatal(e)
	}
	var checkpoint struct {
		OwnedVideoKey       string `json:"ownedVideoKey"`
		Recovered           bool   `json:"recovered"`
		PersistenceAttempts int    `json:"persistenceAttempts"`
	}
	if e := json.Unmarshal(persisted.Output, &checkpoint); e != nil {
		t.Fatal(e)
	}
	if !checkpoint.Recovered || !strings.Contains(checkpoint.OwnedVideoKey, original.ID.String()+".mp4") || persisted.Status != "submitting" {
		t.Fatalf("claim lost atomic recovery checkpoint %+v", checkpoint)
	}
	// Crash immediately after claim: the next lease must recover the persisted owned-video marker.
	if e := d.db.Model(&persisted).Update("lease_until", time.Now().Add(-time.Minute)).Error; e != nil {
		t.Fatal(e)
	}
	reclaimed, e := d.store.Claim(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if reclaimed.Status != "submitting" || string(reclaimed.Output) != string(persisted.Output) {
		t.Fatal("crash recovery lost checkpoint")
	}
	var afterEntries int64
	d.db.Model(&models.DirectorPointEntry{}).Count(&afterEntries)
	if d.balance() != before || entries != afterEntries {
		t.Fatal("recovery changed charges or ledger")
	}
}
func TestDirectorPostgresFrameRecoveryExcludesUnrelatedAndInvalidTargets(t *testing.T) {
	for _, kind := range []string{"other_failure", "refunded", "stale_generation", "deleted_project", "removed_shot", "deleted_job", "wrong_owner"} {
		t.Run(kind, func(t *testing.T) {
			d, j := legacyDirectorFrameJob(t)
			switch kind {
			case "refunded":
				d.db.Model(&j).Update("refunded", true)
			case "deleted_job":
				d.db.Delete(&j)
			case "deleted_project":
				d.db.Model(&models.DirectorProject{}).Where("id=?", j.ProjectID).Update("deleted_at", time.Now())
			case "wrong_owner":
				other := uuid.New()
				if e := d.db.Exec("INSERT INTO users(id) VALUES (?)", other).Error; e != nil {
					t.Fatal(e)
				}
				d.db.Model(&j).Update("user_id", other)
			case "stale_generation":
				d.db.Exec("UPDATE director_projects SET document=jsonb_set(document, ARRAY['generations',?], '\"new-generation\"') WHERE id=?", j.TargetID, j.ProjectID)
			case "removed_shot":
				d.db.Exec("UPDATE director_projects SET document=jsonb_set(document,'{project,scenes,0,shots}','[]') WHERE id=?", j.ProjectID)
			case "other_failure":
				d.db.Exec("UPDATE director_projects SET document=jsonb_set(document,'{project,scenes,0,shots,0,video,failureCode}','\"PROVIDER_TIMEOUT\"') WHERE id=?", j.ProjectID)
			}
			if claimed, e := d.store.Claim(context.Background()); e == nil {
				t.Fatalf("ineligible %s recovery claimed %s", kind, claimed.ID)
			}
		})
	}
}
func TestDirectorPostgresFrameRecoveryAttemptsPersistAndStop(t *testing.T) {
	d, j := legacyDirectorFrameJob(t)
	claimed, e := d.store.Claim(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	var marker map[string]any
	_ = json.Unmarshal(claimed.Output, &marker)
	marker["persistenceAttempts"] = 3
	out, _ := json.Marshal(marker)
	if e = d.store.RecordOutput(context.Background(), claimed, out); e != nil {
		t.Fatal(e)
	}
	if e = d.store.Wait(context.Background(), claimed, out, time.Now().Add(-time.Minute)); e != nil {
		t.Fatal(e)
	}
	next, e := d.store.Claim(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(next.Output), `"persistenceAttempts":3`) {
		t.Fatal("retry count lost across claims")
	}
	if e = d.store.Complete(context.Background(), next, "needs_attention", false, func(p *models.DirectorProject) error {
		var doc map[string]any
		if e := json.Unmarshal(p.Document, &doc); e != nil {
			return e
		}
		scenes := doc["project"].(map[string]any)["scenes"].([]any)
		shots := scenes[0].(map[string]any)["shots"].([]any)
		shots[0].(map[string]any)["video"].(map[string]any)["failureCode"] = "VIDEO_FRAME_RECOVERY_FAILED"
		p.Document, _ = json.Marshal(doc)
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = d.store.Claim(context.Background()); e == nil {
		t.Fatal("terminal frame recovery failure was automatically reentered")
	}
	var entry models.DirectorJob
	if e = d.db.First(&entry, "id=?", j.ID).Error; e != nil {
		t.Fatal(e)
	}
	if entry.Refunded {
		t.Fatal(fmt.Sprintf("generated video was refunded: %s", j.ID))
	}
}

func TestDirectorPostgresFrameRecoveryCompletesLocallyWithoutCharging(t *testing.T) {
	d, j := legacyDirectorFrameJob(t)
	before := d.balance()
	var entries int64
	d.db.Model(&models.DirectorPointEntry{}).Count(&entries)
	path := filepath.Join(t.TempDir(), "fixture.mkv")
	if out, e := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=16x16:r=2:d=1", "-c:v", "ffv1", "-threads", "1", "-y", path).CombinedOutput(); e != nil {
		t.Fatalf("local video fixture: %v %s", e, out)
	}
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	fetches := 0
	// No provider client exists: recovery can only read the previously uploaded owned video.
	f := facade.NewDirectorFacade(d.store, nil, d.media, nil, nil, nil, nil, nil, facade.DirectorOptions{Processor: videoprocessing.NewFFmpegProcessor(""), VideoFetch: func(_ context.Context, u string) ([]byte, string, error) {
		fetches++
		if !strings.Contains(u, j.ID.String()+".mp4") {
			t.Fatal("recovery fetched another job's video")
		}
		return data, "video/mp4", nil
	}})
	if e = f.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	p := d.get(j.ProjectID.String())
	sh := d.shots(p)[0]
	if fetches != 1 || sh.Video.Status != "succeeded" || sh.Video.URL == nil || sh.Video.FailureCode != nil {
		t.Fatalf("legacy video not published: %+v", sh.Video)
	}
	var after models.DirectorJob
	if e = d.db.First(&after, "id=?", j.ID).Error; e != nil {
		t.Fatal(e)
	}
	var afterEntries int64
	d.db.Model(&models.DirectorPointEntry{}).Count(&afterEntries)
	if after.Status != "succeeded" || after.Refunded || d.balance() != before || entries != afterEntries {
		t.Fatal("local recovery changed billing or did not finish")
	}
}
