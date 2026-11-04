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

func TestProgress_IsIndeterminateRegardlessOfHistory(t *testing.T) {
	for _, elapsed := range []time.Duration{0, 20 * time.Second, 10 * time.Minute} {
		job := progressJob(models.JobStatusProcessing, elapsed)
		repo := newFakeJobRepo(job)
		repo.averages = map[string][2]float64{"violet-motion-2": {40, 5}}
		p, step, eta := getProgress(t, repo, job)
		if p != 0 || step != facade_open_api.StepGenerating || eta != nil {
			t.Fatalf("fabricated progress %d %s %v", p, step, eta)
		}
		if repo.averageCalls != 0 {
			t.Fatal("provider-independent estimates must not be queried")
		}
	}
}
func TestProgress_StepsByStatus(t *testing.T) {
	saving := facade_open_api.StepSaving
	for _, tc := range []struct {
		status   models.JobStatus
		current  *string
		progress int
		step     string
	}{
		{models.JobStatusQueued, nil, 0, facade_open_api.StepQueued},
		{models.JobStatusProcessing, &saving, 0, facade_open_api.StepSaving},
		{models.JobStatusSucceeded, nil, 100, facade_open_api.StepDone},
		{models.JobStatusFailed, nil, 0, facade_open_api.StepFailed},
		{models.JobStatusCancelled, nil, 0, facade_open_api.StepCancelled},
	} {
		job := progressJob(tc.status, time.Minute)
		job.CurrentStep = tc.current
		p, step, eta := getProgress(t, newFakeJobRepo(job), job)
		if p != tc.progress || step != tc.step || eta != nil {
			t.Fatalf("status %s: %d %s %v", tc.status, p, step, eta)
		}
	}
}
