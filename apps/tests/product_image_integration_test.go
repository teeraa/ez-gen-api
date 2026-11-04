package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/crons"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type productPipelineGenerator struct {
	data       []byte
	inputs     []modelark.ImageRequest
	onGenerate func()
}

func (g *productPipelineGenerator) GenerateImage(_ context.Context, in modelark.ImageRequest) ([]byte, error) {
	g.inputs = append(g.inputs, in)
	if g.onGenerate != nil {
		g.onGenerate()
	}
	return g.data, nil
}

func TestProductImagePersistenceWorkerAndAsset(t *testing.T) {
	dsn := os.Getenv("VIDEO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires explicitly isolated VIDEO_TEST_DATABASE_URL")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") || (!strings.Contains(parsed.Path, "test") && !strings.Contains(parsed.Path, "qa")) {
		t.Fatal("requires loopback URL for isolated test database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("isolated database unavailable")
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	owner := uuid.New()
	user := models.User{Base: models.Base{ID: owner}, Email: owner.String() + "@productqa.invalid"}
	if err := tx.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	studio, jobs, uploads := repo.NewStudioRepository(tx), repo.NewJobRepository(tx), repo.NewUploadRepository(tx)
	store := storage.NewMemoryClient()
	facade := f.NewStudioFacade(studio, jobs, uploads, store, "isolated-test-fixture", time.Hour, "fake-product-model", 3)
	input := req.CreateImageRequest{Category: "product", Prompt: "A glass bottle with existing blue package label", AspectRatio: "1:1", Mood: "warm", IdempotencyKey: uuid.NewString()}
	created, err := facade.CreateImage(t.Context(), owner.String(), input)
	if err != nil {
		t.Fatal(err)
	}
	job, err := jobs.FindByID(t.Context(), created.JobID, owner.String())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, prompt := string(job.EffectiveInputs), *job.CompiledPrompt
	var recorded struct {
		Category        string          `json:"category"`
		CompilerVersion string          `json:"compilerVersion"`
		CharacterDesign json.RawMessage `json:"characterDesign"`
		ProductDesign   struct {
			VisualStyle string `json:"visualStyle"`
			Mood        string `json:"mood"`
		} `json:"productDesign"`
	}
	if err := json.Unmarshal(job.EffectiveInputs, &recorded); err != nil {
		t.Fatal(err)
	}
	if recorded.Category != "product" || recorded.CompilerVersion != "product-design-v1" || len(recorded.CharacterDesign) != 0 || recorded.ProductDesign.VisualStyle != "auto" || recorded.ProductDesign.Mood != input.Mood {
		t.Fatal("product snapshot lost normalized category, design or compiler provenance")
	}
	again, err := facade.CreateImage(t.Context(), owner.String(), input)
	if err != nil || again.JobID != created.JobID {
		t.Fatalf("replay: %+v %v", again, err)
	}
	changed := input
	changed.Category = "character"
	if _, err := facade.CreateImage(t.Context(), owner.String(), changed); !errors.Is(err, f.ErrIdempotencyMismatch) {
		t.Fatalf("category conflict: %v", err)
	}
	var encoded bytes.Buffer
	pixel := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	pixel.Set(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&encoded, pixel); err != nil {
		t.Fatal(err)
	}
	generator := &productPipelineGenerator{data: encoded.Bytes()}
	generator.onGenerate = func() {
		processing, err := jobs.FindByID(t.Context(), created.JobID, owner.String())
		if err != nil {
			t.Fatal(err)
		}
		if processing.Status != models.JobStatusProcessing || processing.CurrentStep == nil || strings.TrimSpace(*processing.CurrentStep) == "" || strings.Contains(*processing.CurrentStep, "ตัวละคร") {
			t.Fatal("product processing status is missing or labeled as character creation")
		}
	}
	worker := crons.NewImageWorker(studio, uploads, generator, store)
	if err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	completed, err := jobs.FindByID(t.Context(), created.JobID, owner.String())
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != models.JobStatusSucceeded || completed.OutputMediaKey == nil || strings.Contains(*completed.OutputMediaKey, "://") {
		t.Fatalf("not durable succeeded: %+v", completed)
	}
	if completed.CurrentStep == nil || strings.TrimSpace(*completed.CurrentStep) == "" || strings.Contains(*completed.CurrentStep, "ตัวละคร") {
		t.Fatal("product completion is missing or labeled as character creation")
	}
	if len(generator.inputs) != 1 || generator.inputs[0].Prompt != prompt || generator.inputs[0].Model != "fake-product-model" {
		t.Fatal("worker did not use frozen product request")
	}
	if object, ok := store.Object(*completed.OutputMediaKey); !ok || !bytes.Equal(object.Data, encoded.Bytes()) {
		t.Fatal("validated product image not durably stored")
	}
	id := created.JobID
	if _, err := facade.CreateAsset(t.Context(), owner.String(), req.CreateAssetRequest{Name: "Bottle", AssetType: "character", Scope: "personal", JobID: &id}); !errors.Is(err, f.ErrValidation) {
		t.Fatalf("product saved as character: %v", err)
	}
	saved, err := facade.CreateAsset(t.Context(), owner.String(), req.CreateAssetRequest{Name: "Bottle", AssetType: "prop", Scope: "personal", JobID: &id, Prompt: "untrusted replacement"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.AssetType != "prop" || saved.Prompt != input.Prompt || saved.JobID == nil || *saved.JobID != id || saved.Status != "ready" {
		t.Fatalf("asset provenance lost: %+v", saved)
	}
	asset, err := studio.Asset(t.Context(), saved.ID, owner.String())
	if err != nil || asset.StorageKey == nil || *asset.StorageKey != *completed.OutputMediaKey {
		t.Fatalf("asset durable key mismatch: %v", err)
	}
	final, err := jobs.FindByID(t.Context(), id, owner.String())
	if err != nil || string(final.EffectiveInputs) != snapshot || final.CompiledPrompt == nil || *final.CompiledPrompt != prompt {
		t.Fatalf("immutable job mutated: %v", err)
	}
	if err := worker.RunOnce(t.Context()); err != nil || len(generator.inputs) != 1 {
		t.Fatalf("completed product resubmitted: %v", err)
	}
}
