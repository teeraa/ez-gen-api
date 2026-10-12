package tests

import (
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
)

func progressJob(status models.JobStatus, submittedAgo time.Duration) *models.VideoGenerationJob {
	j := newOutputJob(status, submittedAgo)
	j.ModelID, j.DurationSeconds, j.Quality = "violet-motion-2", 5, "standard"
	return j
}

func getProgress(t *testing.T, repo *fakeJobRepo, job *models.VideoGenerationJob) (int, string, *int) {
	t.Helper()
	dto, err := newOutputFacade(repo, modelark.NewMockClient(), storage.NewMemoryClient(), &fakeFetcher{}).
		GetJob(t.Context(), job.ID.String(), job.UserID.String())
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	step := ""
	if dto.CurrentStep != nil {
		step = *dto.CurrentStep
	}
	return dto.Progress, step, dto.EstimatedSecondsRemaining
}

func TestProgress_UsesModelAverageFromHistory(t *testing.T) {
	t.Parallel()
	job := progressJob(models.JobStatusProcessing, 20*time.Second)
	repo := newFakeJobRepo(job)
	repo.averages = map[string][2]float64{"violet-motion-2": {40, 5}}

	progress, step, remaining := getProgress(t, repo, job)
	if step != facade_open_api.StepGenerating {
		t.Errorf("step = %q", step)
	}
	if progress < 43 || progress > 46 {
		t.Errorf("progress = %d, want ~45 at half the 40s average", progress)
	}
	if remaining == nil || *remaining < 18 || *remaining > 21 {
		t.Errorf("remaining = %v, want ~20", remaining)
	}
}

func TestProgress_FallsBackToAnyModelThenDefault(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		averages map[string][2]float64
		expected float64
	}{
		{"too few samples for model", map[string][2]float64{"violet-motion-2": {10, 2}, "": {100, 8}}, 100},
		{"no history", nil, 60},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			job := progressJob(models.JobStatusSubmitted, 0)
			repo := newFakeJobRepo(job)
			repo.averages = tc.averages
			_, _, remaining := getProgress(t, repo, job)
			if remaining == nil || float64(*remaining) < tc.expected-2 || float64(*remaining) > tc.expected {
				t.Errorf("remaining = %v, want ~%v", remaining, tc.expected)
			}
		})
	}
}

func TestProgress_OverrunStaysBelowNinety(t *testing.T) {
	t.Parallel()
	job := progressJob(models.JobStatusProcessing, 10*time.Minute)
	repo := newFakeJobRepo(job)
	repo.averages = map[string][2]float64{"violet-motion-2": {40, 5}}

	progress, _, remaining := getProgress(t, repo, job)
	if progress < 85 || progress >= 90 {
		t.Errorf("progress = %d, want in [85, 90)", progress)
	}
	if remaining == nil || *remaining != 0 {
		t.Errorf("remaining = %v, want 0", remaining)
	}
}

func TestProgress_StepsByStatus(t *testing.T) {
	t.Parallel()
	saving := facade_open_api.StepSaving
	cases := []struct {
		name         string
		status       models.JobStatus
		currentStep  *string
		wantProgress int
		wantStep     string
		wantETA      bool
	}{
		{"queued", models.JobStatusQueued, nil, 2, facade_open_api.StepQueued, true},
		{"saving", models.JobStatusProcessing, &saving, 95, facade_open_api.StepSaving, false},
		{"succeeded", models.JobStatusSucceeded, &saving, 100, facade_open_api.StepDone, false},
		{"failed", models.JobStatusFailed, nil, 0, facade_open_api.StepFailed, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			job := progressJob(tc.status, time.Minute)
			job.CurrentStep = tc.currentStep
			progress, step, remaining := getProgress(t, newFakeJobRepo(job), job)
			if progress != tc.wantProgress || step != tc.wantStep || (remaining != nil) != tc.wantETA {
				t.Errorf("got progress=%d step=%q eta=%v", progress, step, remaining)
			}
		})
	}
}

func TestProgress_CachesAverage(t *testing.T) {
	t.Parallel()
	job := progressJob(models.JobStatusProcessing, 5*time.Second)
	repo := newFakeJobRepo(job)
	repo.averages = map[string][2]float64{"violet-motion-2": {40, 5}}
	f := newOutputFacade(repo, modelark.NewMockClient(), storage.NewMemoryClient(), &fakeFetcher{})

	for range 5 {
		if _, err := f.GetJob(t.Context(), job.ID.String(), job.UserID.String()); err != nil {
			t.Fatal(err)
		}
	}
	if repo.averageCalls != 1 {
		t.Errorf("expected 1 average query across polls, got %d", repo.averageCalls)
	}
}
