package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/google/uuid"
)

type submissionRepo struct {
	*fakeJobRepo
	failures, attempts int
}

func (r *submissionRepo) FindByIdempotencyKey(_ context.Context, key, user string) (*models.VideoGenerationJob, error) {
	for _, j := range r.jobs {
		if j.UserID.String() == user && j.IdempotencyKey != nil && *j.IdempotencyKey == key {
			c := *j
			return &c, nil
		}
	}
	return nil, repo.ErrNotFound
}
func (r *submissionRepo) UpdateStatus(ctx context.Context, id string, status models.JobStatus, fields map[string]any) error {
	r.attempts++
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
		return errors.New("persistence must have a deadline")
	}
	if r.attempts <= r.failures {
		return errors.New("database temporarily unavailable")
	}
	j := r.jobs[id]
	j.Status = status
	task := fields["provider_task_id"].(string)
	j.ProviderTaskID = &task
	return nil
}

type acceptingProvider struct {
	*modelark.MockClient
	cancel context.CancelFunc
}

func (p *acceptingProvider) CreateTask(ctx context.Context, in modelark.CreateTaskRequest) (string, error) {
	id, err := p.MockClient.CreateTask(ctx, in)
	if p.cancel != nil {
		p.cancel()
	}
	return id, err
}
func TestAcceptedSubmissionPersistenceRecovery(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		cancel             bool
		failures, attempts int
		status             models.JobStatus
	}{
		{"canceled caller", true, 0, 1, models.JobStatusSubmitted},
		{"transient database failure", false, 1, 2, models.JobStatusSubmitted},
		{"persistent database failure", false, 99, 3, models.JobStatusQueued},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			jobs := &submissionRepo{fakeJobRepo: newFakeJobRepo(), failures: tc.failures}
			provider := &acceptingProvider{MockClient: modelark.NewMockClient()}
			if tc.cancel {
				provider.cancel = cancel
			}
			registry, _ := modelark.NewRegistry("")
			facade := facade_open_api.NewJobFacade(jobs, nil, nil, nil, nil, nil, provider, registry, storage.NewMemoryClient(), &fakeFetcher{}, 3)
			key, user := uuid.NewString(), uuid.NewString()
			input := facade_open_api.CreateJobInput{Prompt: "A quiet garden", Mode: "text", ModelID: "violet-classic", AspectRatio: "16:9", DurationSeconds: 5, Quality: "standard", IdempotencyKey: &key}
			got, err := facade.CreateJob(ctx, user, input)
			if err != nil {
				t.Fatalf("accepted provider task must return stable job identity: %v", err)
			}
			if got.Status != string(tc.status) {
				t.Fatalf("status = %s, want %s", got.Status, tc.status)
			}
			if jobs.attempts != tc.attempts {
				t.Fatalf("updates = %d, want %d", jobs.attempts, tc.attempts)
			}
			replay, err := facade.CreateJob(t.Context(), user, input)
			if err != nil || replay.JobID != got.JobID || replay.Status != got.Status {
				t.Fatalf("replay = %#v, err = %v", replay, err)
			}
			input.Prompt = "Different paid request"
			if _, err := facade.CreateJob(t.Context(), user, input); !errors.Is(err, facade_open_api.ErrConflict) {
				t.Fatalf("changed request must preserve idempotency conflict, got %v", err)
			}
			if len(provider.CreatedRequests) != 1 {
				t.Fatalf("provider POST count = %d", len(provider.CreatedRequests))
			}
			stored := jobs.jobs[got.JobID]
			if tc.status == models.JobStatusSubmitted && (stored.ProviderTaskID == nil || *stored.ProviderTaskID != provider.NextTaskID) {
				t.Fatal("accepted task ID was not persisted")
			}
		})
	}
}
