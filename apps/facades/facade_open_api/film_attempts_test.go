package facade_open_api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
)

// filmTickStore hands out one job and records lease releases.
type filmTickStore struct {
	*filmFakeStore
	job      models.FilmJob
	released []string
	until    time.Time
}

func (s *filmTickStore) Claim(context.Context) (*models.FilmJob, error) {
	j := s.job
	return &j, nil
}

func (s *filmTickStore) Release(_ context.Context, _ *models.FilmJob, cause string, until time.Time) error {
	s.released, s.until = append(s.released, cause), until
	return nil
}

func filmTickFixture(t *testing.T, kind string, attempts int, snapshot any) (*FilmFacade, *filmTickStore) {
	t.Helper()
	fake := &filmFakeStore{finished: map[uuid.UUID]string{}, refunded: map[uuid.UUID]bool{}}
	fake.project.ID, fake.series.ID = uuid.New(), uuid.New()
	raw, _ := json.Marshal(snapshot)
	project := fake.project.ID
	last := "panic: boom"
	store := &filmTickStore{filmFakeStore: fake, job: models.FilmJob{ID: uuid.New(), UserID: uuid.New(), SeriesID: fake.series.ID,
		ProjectID: &project, Kind: kind, Status: "submitting", Snapshot: raw, Cost: 4, Attempts: attempts, LastError: &last}}
	doc, _ := json.Marshal(filmDocument{Plan: &filmPlanState{Status: "processing", JobID: store.job.ID.String()}})
	fake.episode = models.FilmEpisode{ProjectID: project, Document: doc}
	return NewFilmFacade(store, &filmFakeMedia{uploads: map[string][]byte{}}, nil, nil, nil, FilmOptions{}), store
}

func filmTickPlan(t *testing.T, s *filmTickStore) filmDocument {
	t.Helper()
	ep := s.episode
	d, e := filmDecode(&ep)
	if e != nil {
		t.Fatal(e)
	}
	return d
}

func TestFilmTickRecoversPanicAndReleasesLease(t *testing.T) {
	// The fake store has no Element method, so the element job panics on a nil interface.
	f, store := filmTickFixture(t, "element_image", 1, filmElementJob{ElementID: uuid.NewString()})
	start := time.Now()
	e := f.Tick(context.Background())
	if e == nil || !strings.Contains(e.Error(), "panicked") {
		t.Fatalf("a panic must come back as an error, got %v", e)
	}
	if len(store.released) != 1 || !strings.HasPrefix(store.released[0], "panic: ") {
		t.Fatalf("lease released once with the panic as cause: %q", store.released)
	}
	if d := store.until.Sub(start); d < 2*filmCrashBackoff-time.Second || d > 2*filmCrashBackoff+5*time.Second {
		t.Fatalf("backoff grows with attempts: %v", d)
	}
	if len(store.finished) != 0 {
		t.Fatalf("a crashed job is not finished: %v", store.finished)
	}
}

func TestFilmTickRunsJobAtMaxAttempts(t *testing.T) {
	f, store := filmTickFixture(t, "plan", filmMaxAttempts, filmPlanJob{})
	if e := f.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	d := filmTickPlan(t, store)
	if d.Plan == nil || d.Plan.FailureMessage == nil || *d.Plan.FailureMessage == filmAbandonMessage {
		t.Fatalf("at the limit the job still runs (planner missing → ordinary failure): %+v", d.Plan)
	}
}

func TestFilmTickAbandonsJobAfterRepeatedCrashes(t *testing.T) {
	f, store := filmTickFixture(t, "plan", filmMaxAttempts+1, filmPlanJob{})
	if e := f.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	id := store.job.ID
	if store.finished[id] != "failed" || !store.refunded[id] {
		t.Fatalf("stopped job fails with a refund: status=%q refund=%t", store.finished[id], store.refunded[id])
	}
	d := filmTickPlan(t, store)
	if d.Plan == nil || d.Plan.Status != "failed" || d.Plan.FailureMessage == nil || *d.Plan.FailureMessage != filmAbandonMessage {
		t.Fatalf("plan shows the stop message: %+v", d.Plan)
	}
	if n := len(d.Messages); n == 0 || !strings.Contains(d.Messages[n-1].Text, "คืนแต้ม") {
		t.Fatalf("chat tells the user about the refund: %+v", d.Messages)
	}
}

func TestFilmTickEndsJobWhenAbandonAlsoCrashed(t *testing.T) {
	f, store := filmTickFixture(t, "plan", filmMaxAttempts+2, filmPlanJob{})
	if e := f.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	id := store.job.ID
	if store.finished[id] != "failed" || !store.refunded[id] {
		t.Fatalf("job ends failed with a refund: status=%q refund=%t", store.finished[id], store.refunded[id])
	}
	if d := filmTickPlan(t, store); d.Plan == nil || d.Plan.Status != "processing" {
		t.Fatalf("last-resort stop leaves the target untouched: %+v", d.Plan)
	}
}
