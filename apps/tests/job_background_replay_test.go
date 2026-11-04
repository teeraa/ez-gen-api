package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/google/uuid"
)

func TestBackgroundCompilerChangeDoesNotResubmitExistingVideo(t *testing.T) {
	t.Parallel()
	owner, key, uploadID := uuid.New(), uuid.NewString(), uuid.New()
	upload := uploadID.String()
	in := facade_open_api.CreateJobInput{Prompt: "A person walks at a market", Mode: "image_image", ModelID: "violet-motion-2", AspectRatio: "16:9", DurationSeconds: 5, Quality: "standard", IdempotencyKey: &key, UploadID: &upload}
	encoded, err := json.Marshal(struct {
		Input      facade_open_api.CreateJobInput
		FirstFrame string
	}{Input: in})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(encoded)
	requestHash := hex.EncodeToString(sum[:])
	taskID, oldPrompt := "existing-provider-task", "Old immutable prompt without the new background instruction"
	job := &models.VideoGenerationJob{Base: models.Base{ID: uuid.New()}, UserID: owner, Status: models.JobStatusSubmitted, Prompt: in.Prompt, Mode: in.Mode, ModelID: in.ModelID, AspectRatio: in.AspectRatio, DurationSeconds: in.DurationSeconds, Quality: in.Quality, IdempotencyKey: &key, UploadID: &uploadID, RequestHash: &requestHash, CompiledPrompt: &oldPrompt, ProviderTaskID: &taskID, EffectiveInputs: []byte(`{"assets":[{"uploadId":"legacy","role":"reference"}]}`)}
	snapshot := string(job.EffectiveInputs)
	jobs := &submissionRepo{fakeJobRepo: newFakeJobRepo(job)}
	provider := modelark.NewMockClient()
	registry, _ := modelark.NewRegistry("")
	facade := facade_open_api.NewJobFacade(jobs, nil, nil, nil, nil, nil, provider, registry, nil, nil, 3)
	got, err := facade.CreateJob(t.Context(), owner.String(), in)
	if err != nil || got.JobID != job.ID.String() || got.Status != "submitted" {
		t.Fatalf("existing request no longer replays: %+v %v", got, err)
	}
	if len(provider.CreatedRequests) != 0 || jobs.attempts != 0 || *job.CompiledPrompt != oldPrompt || *job.ProviderTaskID != taskID || string(job.EffectiveInputs) != snapshot {
		t.Fatal("replay resubmitted or changed a stored provider task")
	}
}
