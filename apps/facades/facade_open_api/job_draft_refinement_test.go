package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/google/uuid"
)

func draftJobFixture(t *testing.T) (*jobFacade, *pipelineMemory, *pipelineRecordingArk, string, CreateJobInput, *models.VideoGenerationJob) {
	t.Helper()
	user := uuid.New()
	id := uuid.New()
	provider := "fixture-draft-provider"
	prompt := "walk through garden"
	source := &models.VideoGenerationJob{Base: models.Base{ID: id, CreatedAt: time.Now().Add(-time.Hour)}, UserID: user, Status: models.JobStatusSucceeded, ProviderTaskID: &provider, IsDraft: true, ModelID: "violet-realism", Mode: "image_image", Prompt: prompt, AspectRatio: "9:16", DurationSeconds: 5, Quality: "standard", CompiledPrompt: &prompt}
	source.EffectiveInputs = []byte(`{"providerModelId":"dreamina-seedance-2-5-260628","resolution":"480p","dialogue":"hello"}`)
	memory := &pipelineMemory{js: map[string]*models.VideoGenerationJob{id.String(): source}}
	ark := &pipelineRecordingArk{}
	registry, e := modelark.NewRegistry("")
	if e != nil {
		t.Fatal(e)
	}
	f := &jobFacade{jobs: pipelineJobStore{m: memory}, ark: ark, models: registry, maxActive: 10}
	key := uuid.NewString()
	model, _ := registry.Lookup("violet-realism")
	in := CreateJobInput{draftSourceJobID: id.String(), PinnedModel: &model, Mode: source.Mode, ModelID: source.ModelID, Prompt: source.Prompt, AspectRatio: source.AspectRatio, DurationSeconds: source.DurationSeconds, Quality: "high", IdempotencyKey: &key}
	return f, memory, ark, user.String(), in, source
}

func TestDraftRefinementReusesSourceWithoutReferenceStorage(t *testing.T) {
	f, memory, ark, user, in, source := draftJobFixture(t)
	out, e := f.CreateJob(context.Background(), user, in)
	if e != nil {
		t.Fatal(e)
	}
	if ark.calls != 1 || ark.seen.Draft || ark.seen.Resolution != "1080p" || ark.seen.Ratio != "" || ark.seen.DurationSeconds != 0 || ark.seen.GenerateAudio || ark.seen.OptimizePrompt || len(ark.seen.Content) != 1 || ark.seen.Content[0].DraftTaskID != *source.ProviderTaskID {
		t.Fatalf("bad refinement request %+v", ark.seen)
	}
	final := memory.js[out.JobID]
	if final == nil || final.IsDraft || !source.IsDraft || final.ID == source.ID {
		t.Fatal("draft/final identity damaged")
	}
	var snap struct {
		Source     string `json:"sourceDraftJobId"`
		Resolution string `json:"resolution"`
	}
	_ = json.Unmarshal(final.EffectiveInputs, &snap)
	if snap.Source != source.ID.String() || snap.Resolution != "1080p" {
		t.Fatal(string(final.EffectiveInputs))
	}
	source.CreatedAt = time.Now().Add(-30 * 24 * time.Hour)
	replay, e := f.CreateJob(context.Background(), user, in)
	if e != nil || replay.JobID != out.JobID || ark.calls != 1 {
		t.Fatalf("replay %+v %v", replay, e)
	}
	different := *source
	different.ID = uuid.New()
	memory.js[different.ID.String()] = &different
	in.draftSourceJobID = different.ID.String()
	if _, e = f.CreateJob(context.Background(), user, in); !errors.Is(e, ErrConflict) {
		t.Fatalf("source changed with same key: %v", e)
	}
}

func TestDraftRefinementRejectsInvalidSources(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*models.VideoGenerationJob, *CreateJobInput)
		want   error
	}{
		{"owner", func(s *models.VideoGenerationJob, _ *CreateJobInput) { s.UserID = uuid.New() }, ErrNotFound},
		{"not_draft", func(s *models.VideoGenerationJob, _ *CreateJobInput) { s.IsDraft = false }, ErrValidation},
		{"unfinished", func(s *models.VideoGenerationJob, _ *CreateJobInput) { s.Status = models.JobStatusProcessing }, ErrValidation},
		{"missing_task", func(s *models.VideoGenerationJob, _ *CreateJobInput) { s.ProviderTaskID = nil }, ErrValidation},
		{"expired", func(s *models.VideoGenerationJob, _ *CreateJobInput) {
			s.CreatedAt = time.Now().Add(-30 * 24 * time.Hour)
		}, ErrDraftExpired},
		{"provider_changed", func(s *models.VideoGenerationJob, _ *CreateJobInput) {
			s.EffectiveInputs = []byte(`{"providerModelId":"another-model"}`)
		}, ErrValidation},
		{"missing_provenance", func(s *models.VideoGenerationJob, _ *CreateJobInput) { s.EffectiveInputs = []byte(`{}`) }, ErrValidation},
		{"changed_prompt", func(_ *models.VideoGenerationJob, in *CreateJobInput) { in.Prompt = "different" }, ErrValidation},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, _, ark, user, in, source := draftJobFixture(t)
			test.mutate(source, &in)
			_, e := f.CreateJob(context.Background(), user, in)
			if !errors.Is(e, test.want) {
				t.Fatalf("want %v got %v", test.want, e)
			}
			if ark.calls != 0 {
				t.Fatal("invalid source submitted")
			}
		})
	}
}

func TestDraftJobUses480pAndRecordsProviderProvenance(t *testing.T) {
	f, memory, ark, user, in, _ := draftJobFixture(t)
	in.draftSourceJobID = ""
	in.Mode = "text"
	in.Draft = true
	in.Quality = "standard"
	out, e := f.CreateJob(context.Background(), user, in)
	if e != nil {
		t.Fatal(e)
	}
	if !ark.seen.Draft || ark.seen.Resolution != "480p" {
		t.Fatalf("bad draft %+v", ark.seen)
	}
	job := memory.js[out.JobID]
	if !job.IsDraft {
		t.Fatal("draft state lost")
	}
	var snapshot struct {
		Provider   string `json:"providerModelId"`
		Resolution string `json:"resolution"`
	}
	_ = json.Unmarshal(job.EffectiveInputs, &snapshot)
	if snapshot.Provider != "dreamina-seedance-2-5-260628" || snapshot.Resolution != "480p" {
		t.Fatal(string(job.EffectiveInputs))
	}
}
