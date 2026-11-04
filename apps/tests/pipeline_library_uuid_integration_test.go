package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	h "github.com/ez-gen/ez-gen-api/apps/handlers/handler_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func TestPipelineGeneratedUUIDv5CharacterReusableThroughHTTP(t *testing.T) {
	t.Parallel()
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
	imageFacade := f.NewStudioFacade(studio, jobs, uploads, media, "fixture-only", time.Hour, "fixture-image", 3)
	// Video submission is outside this regression; never run the video stage.
	pipeline := f.NewPipelineFacade(store, imageFacade, nil, jobs, studio, uploads, registry, media, true)
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("userID", owner.String())
		c.Locals("requestID", "library-uuid-regression")
		return c.Next()
	})
	h.NewPipelineHandler(pipeline, 100).RegisterRoutes(app)
	input := req.CreatePipelineRequest{CharacterPrompt: "Synthetic character in a cream jacket", ScenePrompt: "Walk through a garden", ModelID: "violet-realism", IdempotencyKey: uuid.NewString()}
	body, _ := json.Marshal(input)
	status, raw := pipelineHTTP(t, app, "POST", "/studio-pipelines", body)
	if status != 201 {
		t.Fatalf("text pipeline %d %s", status, raw)
	}
	var out struct {
		Data dto.PipelineDTO `json:"data"`
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	sourceID := out.Data.ID
	if err = pipeline.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if store.p.CharacterJobID == nil {
		t.Fatal("image job not enqueued")
	}
	// Fake image-provider completion: production facade and saveAsset remain real.
	key, kind := "characters/uuid-library-fixture.png", "image"
	if err = jobs.UpdateStatus(ctx, store.p.CharacterJobID.String(), models.JobStatusSucceeded, map[string]any{"output_media_key": key, "output_media_type": kind}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = pipeline.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	status, raw = pipelineHTTP(t, app, "GET", "/studio-pipelines/"+sourceID, nil)
	if status != 200 {
		t.Fatalf("source GET %d %s", status, raw)
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Data.CharacterAssetID == nil {
		t.Fatal("real saveAsset did not produce character")
	}
	characterID := *out.Data.CharacterAssetID
	parsed, err := uuid.Parse(characterID)
	if err != nil || parsed.Version() != 5 {
		t.Fatalf("expected actual saveAsset UUIDv5 got %s (%v)", characterID, err)
	}
	library := req.CreatePipelineRequest{CharacterAssetID: characterID, ScenePrompt: "Wave at the camera", ModelID: "violet-realism", IdempotencyKey: uuid.NewString()}
	body, _ = json.Marshal(library)
	status, raw = pipelineHTTP(t, app, "POST", "/studio-pipelines", body)
	if status != 201 {
		t.Fatalf("own generated UUIDv5 character rejected %d %s", status, raw)
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Data.SourceMode != "library" || out.Data.Stage != "video" || out.Data.CharacterAssetID == nil || *out.Data.CharacterAssetID != characterID {
		t.Fatalf("character not reused %s", raw)
	}
	if len(jobs.jobs) != 1 || len(base.assets) != 1 {
		t.Fatal("library reuse created additional image generation")
	}
	// Syntax acceptance must still reach facade authorization, not bypass it.
	foreignID := uuid.NewSHA1(uuid.New(), []byte("foreign-character"))
	foreign := *base.assets[characterID]
	foreign.ID = foreignID
	foreign.UserID = uuid.New()
	base.assets[foreignID.String()] = &foreign
	unknownID := uuid.NewSHA1(uuid.New(), []byte("missing-character"))
	for _, id := range []uuid.UUID{foreignID, unknownID, uuid.Nil} {
		library.CharacterAssetID = id.String()
		library.IdempotencyKey = uuid.NewString()
		body, _ = json.Marshal(library)
		status, raw = pipelineHTTP(t, app, "POST", "/studio-pipelines", body)
		if status != 404 {
			t.Fatalf("missing/foreign UUIDv5 %s got %d %s", id, status, raw)
		}
	}
	for _, bad := range []string{"not-a-uuid", strings.ReplaceAll(characterID, "-", ""), "urn:uuid:" + characterID} {
		library.CharacterAssetID = bad
		library.IdempotencyKey = uuid.NewString()
		body, _ = json.Marshal(library)
		status, raw = pipelineHTTP(t, app, "POST", "/studio-pipelines", body)
		if status != 400 || !bytes.Contains(raw, []byte(`"reason":"invalid_uuid"`)) {
			t.Fatalf("malformed/noncanonical character %q got %d %s", bad, status, raw)
		}
	}

}
