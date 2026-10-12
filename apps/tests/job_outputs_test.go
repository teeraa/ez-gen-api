package tests

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/google/uuid"
)

const (
	providerVideoURL = "https://ark-content.tos-ap-southeast-1.volces.com/out.mp4?X-Tos-Signature=abc"
	providerThumbURL = "https://ark-content.tos-ap-southeast-1.volces.com/out_last-frame.png?X-Tos-Signature=def"
)

type fakeJobRepo struct {
	repository_open_api.JobRepository
	mu            sync.Mutex
	jobs          map[string]*models.VideoGenerationJob
	statusUpdates []map[string]any
	statuses      []models.JobStatus
}

func newFakeJobRepo(jobs ...*models.VideoGenerationJob) *fakeJobRepo {
	r := &fakeJobRepo{jobs: make(map[string]*models.VideoGenerationJob)}
	for _, j := range jobs {
		r.jobs[j.ID.String()] = j
	}
	return r
}

func (r *fakeJobRepo) FindByID(_ context.Context, id, userID string) (*models.VideoGenerationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok || j.UserID.String() != userID {
		return nil, repository_open_api.ErrNotFound
	}
	c := *j
	return &c, nil
}

func (r *fakeJobRepo) UpdatePolled(context.Context, string, time.Time) error { return nil }

func (r *fakeJobRepo) UpdateStatus(_ context.Context, _ string, status models.JobStatus, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses = append(r.statuses, status)
	r.statusUpdates = append(r.statusUpdates, updates)
	return nil
}

func (r *fakeJobRepo) ListUnpersistedOutputs(context.Context, int) ([]models.VideoGenerationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.VideoGenerationJob
	for _, j := range r.jobs {
		if j.Status == models.JobStatusSucceeded && j.OutputMediaKey != nil && strings.HasPrefix(*j.OutputMediaKey, "http") {
			out = append(out, *j)
		}
	}
	return out, nil
}

func (r *fakeJobRepo) UpdateOutputKeys(_ context.Context, id string, mediaKey string, thumbKey *string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j := r.jobs[id]
	j.OutputMediaKey = &mediaKey
	j.OutputThumbKey = thumbKey
	return nil
}

type fakeFetcher struct {
	err   error
	calls []string
	mu    sync.Mutex
}

func (f *fakeFetcher) Fetch(_ context.Context, rawURL string) ([]byte, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, rawURL)
	if f.err != nil {
		return nil, "", f.err
	}
	if strings.Contains(rawURL, ".png") {
		return []byte("png-bytes"), "image/png", nil
	}
	return []byte("mp4-bytes"), "video/mp4", nil
}

func newOutputJob(status models.JobStatus, submittedAgo time.Duration) *models.VideoGenerationJob {
	taskID := "cgt-task-1"
	submitted := time.Now().Add(-submittedAgo)
	j := &models.VideoGenerationJob{
		UserID:         uuid.New(),
		Status:         status,
		ProviderTaskID: &taskID,
		SubmittedAt:    &submitted,
	}
	j.ID = uuid.New()
	return j
}

func newOutputFacade(repo *fakeJobRepo, ark *modelark.MockClient, store *storage.MemoryClient, fetcher *fakeFetcher) facade_open_api.JobFacade {
	registry, _ := modelark.NewRegistry("")
	return facade_open_api.NewJobFacade(repo, nil, nil, nil, nil, nil, ark, registry, store, fetcher, 3)
}

func TestPollAndUpdate_PersistsOutputsToStorage(t *testing.T) {
	t.Parallel()
	job := newOutputJob(models.JobStatusProcessing, time.Minute)
	repo := newFakeJobRepo(job)
	ark := modelark.NewMockClient()
	ark.SetTask("cgt-task-1", &modelark.TaskResult{
		Status:       modelark.ProviderStatusSucceeded,
		VideoURL:     providerVideoURL,
		LastFrameURL: providerThumbURL,
	})
	store := storage.NewMemoryClient()

	if err := newOutputFacade(repo, ark, store, &fakeFetcher{}).PollAndUpdate(t.Context(), job); err != nil {
		t.Fatalf("poll: %v", err)
	}

	if len(repo.statuses) != 1 || repo.statuses[0] != models.JobStatusSucceeded {
		t.Fatalf("expected one succeeded transition, got %v", repo.statuses)
	}
	updates := repo.statusUpdates[0]
	wantVideo := storage.KeyForOutput(job.UserID.String(), job.ID.String(), "mp4")
	if updates["output_media_key"] != wantVideo {
		t.Errorf("output_media_key = %v, want %q", updates["output_media_key"], wantVideo)
	}
	thumb, ok := updates["output_thumb_key"].(*string)
	if !ok || thumb == nil || *thumb != storage.KeyForThumbnail(job.UserID.String(), job.ID.String(), "png") {
		t.Errorf("unexpected output_thumb_key: %v", updates["output_thumb_key"])
	}
	if obj, ok := store.Object(wantVideo); !ok || string(obj.Data) != "mp4-bytes" || obj.ContentType != "video/mp4" {
		t.Errorf("video not stored correctly: %+v", obj)
	}
}

func TestPollAndUpdate_StorageFailureRetriesLater(t *testing.T) {
	t.Parallel()
	job := newOutputJob(models.JobStatusProcessing, time.Minute)
	repo := newFakeJobRepo(job)
	ark := modelark.NewMockClient()
	ark.SetTask("cgt-task-1", &modelark.TaskResult{Status: modelark.ProviderStatusSucceeded, VideoURL: providerVideoURL})

	err := newOutputFacade(repo, ark, storage.NewMemoryClient(), &fakeFetcher{err: errors.New("timeout")}).PollAndUpdate(t.Context(), job)
	if err == nil {
		t.Fatal("expected error so the poller retries")
	}
	if len(repo.statuses) != 0 {
		t.Errorf("job must stay in processing, got transitions %v", repo.statuses)
	}
}

func TestPollAndUpdate_ExpiredOutputMarksFailed(t *testing.T) {
	t.Parallel()
	job := newOutputJob(models.JobStatusProcessing, 25*time.Hour)
	repo := newFakeJobRepo(job)
	ark := modelark.NewMockClient()
	ark.SetTask("cgt-task-1", &modelark.TaskResult{Status: modelark.ProviderStatusSucceeded, VideoURL: providerVideoURL})

	if err := newOutputFacade(repo, ark, storage.NewMemoryClient(), &fakeFetcher{err: errors.New("status 403")}).PollAndUpdate(t.Context(), job); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(repo.statuses) != 1 || repo.statuses[0] != models.JobStatusFailed {
		t.Fatalf("expected failed transition, got %v", repo.statuses)
	}
	if repo.statusUpdates[0]["failure_code"] != "OUTPUT_EXPIRED" {
		t.Errorf("unexpected failure_code: %v", repo.statusUpdates[0]["failure_code"])
	}
	if _, leaked := repo.statusUpdates[0]["output_media_key"]; leaked {
		t.Error("provider URL must not be stored")
	}
}

func TestGetJob_ReturnsSignedStorageURL(t *testing.T) {
	t.Parallel()
	job := newOutputJob(models.JobStatusSucceeded, time.Minute)
	key := storage.KeyForOutput(job.UserID.String(), job.ID.String(), "mp4")
	job.OutputMediaKey = &key
	repo := newFakeJobRepo(job)

	dto, err := newOutputFacade(repo, modelark.NewMockClient(), storage.NewMemoryClient(), &fakeFetcher{}).
		GetJob(t.Context(), job.ID.String(), job.UserID.String())
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if dto.Result == nil || dto.Result.MediaURL != "https://storage.test/signed/"+key {
		t.Fatalf("unexpected result: %+v", dto.Result)
	}
	if dto.Result.ExpiresAt == nil {
		t.Error("expected expiresAt for a signed URL")
	}
}

func TestBackfillOutputs_MovesProviderURLsToStorage(t *testing.T) {
	t.Parallel()
	legacy := newOutputJob(models.JobStatusSucceeded, 2*time.Hour)
	video, thumb := providerVideoURL, providerThumbURL
	legacy.OutputMediaKey, legacy.OutputThumbKey = &video, &thumb
	repo := newFakeJobRepo(legacy)
	store := storage.NewMemoryClient()

	n, err := newOutputFacade(repo, modelark.NewMockClient(), store, &fakeFetcher{}).BackfillOutputs(t.Context(), 10)
	if err != nil || n != 1 {
		t.Fatalf("backfill = %d, %v; want 1, nil", n, err)
	}
	got := repo.jobs[legacy.ID.String()]
	if *got.OutputMediaKey != storage.KeyForOutput(legacy.UserID.String(), legacy.ID.String(), "mp4") {
		t.Errorf("media key not replaced: %q", *got.OutputMediaKey)
	}
	if got.OutputThumbKey == nil || strings.HasPrefix(*got.OutputThumbKey, "http") {
		t.Errorf("thumb key not replaced: %v", got.OutputThumbKey)
	}
}

func (r *fakeJobRepo) Create(_ context.Context, job *models.VideoGenerationJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	c := *job
	r.jobs[job.ID.String()] = &c
	return nil
}

func (r *fakeJobRepo) CountActiveByUser(context.Context, string) (int64, error) { return 0, nil }

type fakeSceneRepo struct {
	repository_open_api.SceneRepository
	mu     sync.Mutex
	scenes map[string]*models.Scene
}

func (r *fakeSceneRepo) FindByID(_ context.Context, id, userID string) (*models.Scene, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.scenes[id]
	if !ok || s.UserID.String() != userID {
		return nil, repository_open_api.ErrNotFound
	}
	c := *s
	return &c, nil
}

func (r *fakeSceneRepo) Create(_ context.Context, scene *models.Scene) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	scene.ID = uuid.New()
	c := *scene
	r.scenes[scene.ID.String()] = &c
	return nil
}

func TestContinueScene_UsesPreviousLastFrameAsFirstFrame(t *testing.T) {
	t.Parallel()
	prevJob := newOutputJob(models.JobStatusSucceeded, time.Hour)
	thumbKey := storage.KeyForThumbnail(prevJob.UserID.String(), prevJob.ID.String(), "png")
	prevJob.OutputThumbKey = &thumbKey
	jobs := newFakeJobRepo(prevJob)

	prevScene := &models.Scene{UserID: prevJob.UserID, SelectedJobID: &prevJob.ID}
	prevScene.ID = uuid.New()
	scenes := &fakeSceneRepo{scenes: map[string]*models.Scene{prevScene.ID.String(): prevScene}}

	ark := modelark.NewMockClient()
	registry, _ := modelark.NewRegistry("seedance-1-0-pro-250528")
	f := facade_open_api.NewJobFacade(jobs, nil, nil, nil, scenes, nil, ark, registry,
		storage.NewMemoryClient(), &fakeFetcher{}, 3)

	_, err := f.ContinueScene(t.Context(), prevScene.ID.String(), prevJob.UserID.String(), facade_open_api.ContinueSceneInput{
		ActionOverride:  "she walks to the river",
		ModelID:         "violet-motion-2",
		AspectRatio:     "16:9",
		DurationSeconds: 5,
		Quality:         "standard",
		UseLastFrame:    true,
	})
	if err != nil {
		t.Fatalf("continue scene: %v", err)
	}
	if len(ark.CreatedRequests) != 1 {
		t.Fatalf("expected one provider task, got %d", len(ark.CreatedRequests))
	}
	req := ark.CreatedRequests[0]
	if req.Model != "seedance-1-0-pro-250528" {
		t.Errorf("model = %q, want ARK_DEFAULT_MODEL override", req.Model)
	}
	var frame *modelark.ContentItem
	for i := range req.Content {
		if req.Content[i].Role == modelark.RoleFirstFrame {
			frame = &req.Content[i]
		}
	}
	if frame == nil {
		t.Fatalf("no first_frame content item in %+v", req.Content)
	}
	if frame.URL != "https://storage.test/signed/"+thumbKey {
		t.Errorf("first frame url = %q, want signed storage url", frame.URL)
	}
}

func TestContinueScene_WithoutLastFrameIsTextOnly(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	prevScene := &models.Scene{UserID: userID}
	prevScene.ID = uuid.New()
	scenes := &fakeSceneRepo{scenes: map[string]*models.Scene{prevScene.ID.String(): prevScene}}
	ark := modelark.NewMockClient()
	registry, _ := modelark.NewRegistry("")
	f := facade_open_api.NewJobFacade(newFakeJobRepo(), nil, nil, nil, scenes, nil, ark, registry,
		storage.NewMemoryClient(), &fakeFetcher{}, 3)

	_, err := f.ContinueScene(t.Context(), prevScene.ID.String(), userID.String(), facade_open_api.ContinueSceneInput{
		ActionOverride: "next shot", ModelID: "violet-classic", AspectRatio: "16:9",
		DurationSeconds: 5, Quality: "standard", UseLastFrame: true,
	})
	if err != nil {
		t.Fatalf("continue scene: %v", err)
	}
	for _, item := range ark.CreatedRequests[0].Content {
		if item.Role == modelark.RoleFirstFrame {
			t.Error("unexpected first_frame when previous scene has no selected output")
		}
	}
}
