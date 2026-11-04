package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	h "github.com/ez-gen/ez-gen-api/apps/handlers/handler_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Keep completed aggregates so final rendering cannot overwrite its source.
// The embedded store handles leases and assets for the currently running aggregate.
type draftHTTPPipelineStore struct {
	*httpPipelineStore
	archived map[string]*models.StudioPipeline
}

func (s *draftHTTPPipelineStore) Create(_ context.Context, p *models.StudioPipeline) (*models.StudioPipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p != nil {
		if s.p.IdempotencyKey == p.IdempotencyKey && s.p.UserID == p.UserID {
			return copyHTTPPipeline(s.p), nil
		}
		s.archived[s.p.ID.String()] = copyHTTPPipeline(s.p)
	}
	p.ID = uuid.New()
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt
	s.p = copyHTTPPipeline(p)
	return copyHTTPPipeline(p), nil
}
func (s *draftHTTPPipelineStore) Get(_ context.Context, id, user string) (*models.StudioPipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.archived[id]
	if s.p != nil && s.p.ID.String() == id {
		p = s.p
	}
	if p == nil || p.UserID.String() != user {
		return nil, repo.ErrNotFound
	}
	return copyHTTPPipeline(p), nil
}
func (s *draftHTTPPipelineStore) ByKey(_ context.Context, key, user string) (*models.StudioPipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p != nil && s.p.UserID.String() == user && s.p.IdempotencyKey == key {
		return copyHTTPPipeline(s.p), nil
	}
	for _, p := range s.archived {
		if p.UserID.String() == user && p.IdempotencyKey == key {
			return copyHTTPPipeline(p), nil
		}
	}
	return nil, repo.ErrNotFound
}

func TestPipelineDraftToFinalHTTPActualJobFacadeIntegration(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe required for actual video persistence")
	}
	ctx := context.Background()
	owner := uuid.New()
	base := &httpPipelineStore{assets: map[string]*models.LibraryAsset{}}
	store := &draftHTTPPipelineStore{httpPipelineStore: base, archived: map[string]*models.StudioPipeline{}}
	jobs := &httpJobRepo{newFakeJobRepo()}
	studio := &httpStudioRepo{s: base, j: jobs}
	uploads := &httpUploadRepo{upload: &models.Upload{Base: models.Base{ID: uuid.New()}, UserID: owner}}
	media := storage.NewMemoryClient()
	registry, err := modelark.NewRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	ark := modelark.NewMockClient()
	ark.NextTaskID = "fixture-draft-task"
	video := f.NewJobFacade(jobs, uploads, nil, nil, nil, nil, ark, registry, media, &fakeFetcher{}, 3)
	f.ConfigureJobPipeline(video, studio)
	images := f.NewStudioFacade(studio, jobs, uploads, media, "fixture-only", time.Hour, "fixture-image", 3)
	pipeline := f.NewPipelineFacade(store, images, video, jobs, studio, uploads, registry, media, true)
	assetID, characterJobID := uuid.New(), uuid.New()
	characterKey, kind := "characters/draft-fixture.png", "image"
	jobs.jobs[characterJobID.String()] = &models.VideoGenerationJob{Base: models.Base{ID: characterJobID}, UserID: owner, Mode: "text_image", Status: models.JobStatusSucceeded, OutputMediaKey: &characterKey, OutputMediaType: &kind, EffectiveInputs: []byte(`{"category":"character"}`)}
	base.assets[assetID.String()] = &models.LibraryAsset{Base: models.Base{ID: assetID}, UserID: owner, AssetType: "character", JobID: &characterJobID, StorageKey: &characterKey, MediaType: &kind}
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		user := owner.String()
		if c.Get("X-Fixture-Foreign") == "1" {
			user = uuid.NewString()
		}
		c.Locals("userID", user)
		c.Locals("requestID", uuid.NewString())
		return c.Next()
	})
	h.NewPipelineHandler(pipeline, 100).RegisterRoutes(app)
	readPipeline := func(method, path string, body []byte) dto.PipelineDTO {
		t.Helper()
		status, raw := pipelineHTTP(t, app, method, path, body)
		if status < 200 || status >= 300 {
			t.Fatalf("%s %s: %d %s", method, path, status, raw)
		}
		var out struct {
			Data dto.PipelineDTO `json:"data"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out.Data
	}
	status, raw := pipelineHTTP(t, app, "GET", "/studio-pipeline-capabilities", nil)
	if status != 200 {
		t.Fatalf("caps %d %s", status, raw)
	}
	var caps struct {
		Data dto.PipelineCapabilitiesDTO `json:"data"`
	}
	if err := json.Unmarshal(raw, &caps); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range caps.Data.Models {
		if m.ID == "violet-realism" {
			found = m.SupportsDraft && strings.Contains(m.Label, "Seedance 2.5") && len(m.LibraryDurations) > 0
		}
	}
	if !found {
		t.Fatalf("resolved draft model absent %s", raw)
	}
	input := req.CreatePipelineRequest{CharacterAssetID: assetID.String(), ScenePrompt: "Walk slowly through a garden", ModelID: "violet-realism", AspectRatio: "9:16", DurationSeconds: 5, Quality: "standard", Draft: true, IdempotencyKey: uuid.NewString()}
	body, _ := json.Marshal(input)
	draft := readPipeline("POST", "/studio-pipelines", body)
	if !draft.IsDraft || draft.CanCreateFinal || draft.Quality != "standard" {
		t.Fatalf("queued draft metadata %+v", draft)
	}
	if err := pipeline.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ark.CreatedRequests) != 1 {
		t.Fatal("draft task count", len(ark.CreatedRequests))
	}
	wireDraft := ark.CreatedRequests[0]
	if !wireDraft.Draft || wireDraft.Resolution != "480p" || wireDraft.Model != "dreamina-seedance-2-5-260628" {
		t.Fatalf("draft provider request %+v", wireDraft)
	}
	ark.SetTask("fixture-draft-task", &modelark.TaskResult{ProviderTaskID: "fixture-draft-task", Status: modelark.ProviderStatusSucceeded, VideoURL: "https://fixture.invalid/draft.mp4", Resolution: "480p"})
	complete := func() {
		t.Helper()
		job, err := jobs.FindByID(ctx, store.p.VideoJobID.String(), owner.String())
		if err != nil {
			t.Fatal(err)
		}
		if err = video.PollAndUpdate(ctx, job); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			if err = pipeline.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	complete()
	draft = readPipeline("GET", "/studio-pipelines/"+draft.ID, nil)
	if draft.Status != "succeeded" || !draft.IsDraft || !draft.CanCreateFinal || draft.VideoAssetID == nil || draft.DraftExpiresAt == nil {
		t.Fatalf("completed draft %+v", draft)
	}
	// The HTTP boundary must reject a source that belongs to another user.
	foreign := httptest.NewRequest("POST", "/studio-pipelines/"+draft.ID+"/finalize", strings.NewReader(fmt.Sprintf(`{"idempotencyKey":%q}`, uuid.NewString())))
	foreign.Header.Set("Content-Type", "application/json")
	foreign.Header.Set("X-Fixture-Foreign", "1")
	response, err := app.Test(foreign)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatal("foreign draft status", response.StatusCode)
	}
	ark.NextTaskID = "fixture-final-task"
	finalBody := []byte(fmt.Sprintf(`{"idempotencyKey":%q}`, uuid.NewString()))
	final := readPipeline("POST", "/studio-pipelines/"+draft.ID+"/finalize", finalBody)
	if final.IsDraft || final.Quality != "high" || final.SourceDraftID == nil || *final.SourceDraftID != draft.ID || final.ID == draft.ID || final.CharacterAssetID == nil || *final.CharacterAssetID != assetID.String() {
		t.Fatalf("new final metadata %+v", final)
	}
	if err = pipeline.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ark.CreatedRequests) != 2 {
		t.Fatal("final task count", len(ark.CreatedRequests))
	}
	wireFinal := ark.CreatedRequests[1]
	if wireFinal.Draft || wireFinal.Resolution != "1080p" || wireFinal.Model != wireDraft.Model || len(wireFinal.Content) != 1 || wireFinal.Content[0].Type != modelark.ContentTypeDraftTask || wireFinal.Content[0].DraftTaskID != "fixture-draft-task" || wireFinal.Ratio != "" || wireFinal.DurationSeconds != 0 || wireFinal.GenerateAudio || wireFinal.OptimizePrompt {
		t.Fatalf("refinement repeated inputs or lost source %+v", wireFinal)
	}
	ark.SetTask("fixture-final-task", &modelark.TaskResult{ProviderTaskID: "fixture-final-task", Status: modelark.ProviderStatusSucceeded, VideoURL: "https://fixture.invalid/final.mp4", Resolution: "1080p"})
	complete()
	final = readPipeline("GET", "/studio-pipelines/"+final.ID, nil)
	if final.Status != "succeeded" || final.IsDraft || final.VideoAssetID == nil || *final.VideoAssetID == *draft.VideoAssetID {
		t.Fatalf("final output not independent %+v", final)
	}
	replay := readPipeline("POST", "/studio-pipelines/"+draft.ID+"/finalize", finalBody)
	if replay.ID != final.ID || len(ark.CreatedRequests) != 2 {
		t.Fatal("final replay duplicated task")
	}
	restoredDraft := readPipeline("GET", "/studio-pipelines/"+draft.ID, nil)
	if !restoredDraft.IsDraft || restoredDraft.Status != "succeeded" || restoredDraft.VideoAssetID == nil || *restoredDraft.VideoAssetID != *draft.VideoAssetID {
		t.Fatal("source draft changed")
	}
}
