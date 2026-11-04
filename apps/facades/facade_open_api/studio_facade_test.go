package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/characterdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

type assetRepoFake struct {
	repo.StudioRepository
	saved  *models.LibraryAsset
	queued *models.VideoGenerationJob
}

func (r *assetRepoFake) SaveAsset(_ context.Context, a *models.LibraryAsset) error {
	r.saved = a
	a.ID = uuid.New()
	return nil
}
func (r *assetRepoFake) EnqueueImage(_ context.Context, j *models.VideoGenerationJob, _ int64) (*models.VideoGenerationJob, error) {
	if r.queued != nil {
		return r.queued, nil
	}
	j.ID = uuid.New()
	r.queued = j
	return j, nil
}

type assetJobFake struct {
	repo.JobRepository
	job *models.VideoGenerationJob
}

func (r *assetJobFake) FindByIdempotencyKey(_ context.Context, key, user string) (*models.VideoGenerationJob, error) {
	if r.job != nil && r.job.UserID.String() == user && r.job.IdempotencyKey != nil && *r.job.IdempotencyKey == key {
		return r.job, nil
	}
	return nil, repo.ErrNotFound
}
func (r *assetJobFake) FindByID(_ context.Context, id, user string) (*models.VideoGenerationJob, error) {
	if r.job == nil || r.job.ID.String() != id || r.job.UserID.String() != user {
		return nil, repo.ErrNotFound
	}
	return r.job, nil
}
func TestAssetSaveRequiresOwnedCompletedDurableImage(t *testing.T) {
	owner := uuid.New()
	key := "outputs/owner/job/character.png"
	kind := "image"
	j := &models.VideoGenerationJob{Base: models.Base{ID: uuid.New()}, UserID: owner, Status: models.JobStatusSucceeded, Prompt: "original authoritative prompt", OutputMediaKey: &key, OutputMediaType: &kind}
	for _, tc := range []struct {
		name, user string
		status     models.JobStatus
		media, key string
		want       error
	}{{"valid", owner.String(), models.JobStatusSucceeded, "image", key, nil}, {"foreign", uuid.NewString(), models.JobStatusSucceeded, "image", key, ErrNotFound}, {"not completed", owner.String(), models.JobStatusProcessing, "image", key, ErrAssetSource}, {"video", owner.String(), models.JobStatusSucceeded, "video", key, ErrAssetSource}, {"ephemeral URL", owner.String(), models.JobStatusSucceeded, "image", "https://provider/output", ErrAssetSource}} {
		t.Run(tc.name, func(t *testing.T) {
			copy := *j
			copy.Status = tc.status
			copy.OutputMediaType = &tc.media
			copy.OutputMediaKey = &tc.key
			r := &assetRepoFake{}
			f := NewStudioFacade(r, &assetJobFake{job: &copy}, nil, storage.NewMemoryClient(), "fixture", time.Hour, "model", 3)
			id := j.ID.String()
			d, e := f.CreateAsset(context.Background(), tc.user, req.CreateAssetRequest{Name: "Hero", AssetType: "character", Scope: "personal", Prompt: "tampered", JobID: &id})
			if !errors.Is(e, tc.want) {
				t.Fatalf("got %v want %v", e, tc.want)
			}
			if tc.want == nil {
				if d.MediaURL == nil || d.Status != "ready" || d.Prompt != j.Prompt || r.saved == nil {
					t.Fatalf("invalid saved asset %+v", d)
				}
			} else if r.saved != nil {
				t.Fatal("invalid source persisted")
			}
		})
	}
}
func TestImageSubmissionIdempotencyAndSnapshot(t *testing.T) {
	r := &assetRepoFake{}
	f := NewStudioFacade(r, nil, nil, nil, "fixture", time.Hour, "activated-image-model", 3)
	input := req.CreateImageRequest{Prompt: "A character", AspectRatio: "1:1", IdempotencyKey: uuid.NewString()}
	a, e := f.CreateImage(context.Background(), uuid.NewString(), input)
	if e != nil {
		t.Fatal(e)
	}
	b, e := f.CreateImage(context.Background(), r.queued.UserID.String(), input)
	if e != nil || a.JobID != b.JobID {
		t.Fatalf("repeat %v %v", b, e)
	}
	if string(r.queued.EffectiveInputs) == "" || r.queued.Mode != "text_image" || r.queued.CompiledPrompt == nil || !strings.Contains(*r.queued.CompiledPrompt, input.Prompt) || *r.queued.CompiledPrompt == input.Prompt {
		t.Fatal("missing snapshot")
	}
	input.Prompt = "Different"
	if _, e = f.CreateImage(context.Background(), r.queued.UserID.String(), input); !errors.Is(e, ErrIdempotencyMismatch) {
		t.Fatalf("mismatched retry accepted: %v", e)
	}
}

func TestImageDesignOptionsIdempotencyAndValidation(t *testing.T) {
	t.Parallel()
	owner := uuid.NewString()
	for _, field := range []string{"style", "mood", "format"} {
		t.Run(field, func(t *testing.T) {
			r := &assetRepoFake{}
			f := NewStudioFacade(r, nil, nil, nil, "fixture", time.Hour, "model", 3)
			in := req.CreateImageRequest{Prompt: "ตัวละครผมเงิน", AspectRatio: "3:4", IdempotencyKey: uuid.NewString()}
			if _, err := f.CreateImage(context.Background(), owner, in); err != nil {
				t.Fatal(err)
			}
			var snapshot struct {
				CharacterDesign characterdesign.Options `json:"characterDesign"`
				CompilerVersion string                  `json:"compilerVersion"`
			}
			if err := json.Unmarshal(r.queued.EffectiveInputs, &snapshot); err != nil || snapshot.CompilerVersion != characterdesign.CompilerVersion || snapshot.CharacterDesign.DesignFormat != "full_body" {
				t.Fatalf("snapshot missing options: %s", r.queued.EffectiveInputs)
			}
			same := in
			same.VisualStyle = "auto"
			same.Mood = "neutral"
			same.DesignFormat = "full_body"
			if _, err := f.CreateImage(context.Background(), owner, same); err != nil {
				t.Fatalf("explicit defaults conflict: %v", err)
			}
			changed := in
			switch field {
			case "style":
				changed.VisualStyle = "anime"
			case "mood":
				changed.Mood = "warm"
			case "format":
				changed.DesignFormat = "portrait"
			}
			if _, err := f.CreateImage(context.Background(), owner, changed); !errors.Is(err, ErrIdempotencyMismatch) {
				t.Fatalf("changed tags reused key: %v", err)
			}
			invalid := in
			switch field {
			case "style":
				invalid.VisualStyle = "unknown"
			case "mood":
				invalid.Mood = "unknown"
			case "format":
				invalid.DesignFormat = "unknown"
			}
			if _, err := f.CreateImage(context.Background(), owner, invalid); !errors.Is(err, ErrValidation) {
				t.Fatalf("invalid enum accepted: %v", err)
			}
		})
	}
}
func TestLegacyImageRetryKeepsOriginalPromptAndSnapshot(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	key := uuid.NewString()
	prompt := "original character"
	legacy := &models.VideoGenerationJob{Base: models.Base{ID: uuid.New()}, UserID: owner, Prompt: prompt, CompiledPrompt: &prompt, AspectRatio: "1:1", Mode: "text_image", EffectiveInputs: []byte(`{"model":"old-model"}`), IdempotencyKey: &key}
	r := &assetRepoFake{queued: legacy}
	f := NewStudioFacade(r, nil, nil, nil, "fixture", time.Hour, "new-model", 3)
	if _, err := f.CreateImage(context.Background(), owner.String(), req.CreateImageRequest{Prompt: prompt, AspectRatio: "1:1", IdempotencyKey: key}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.CreateImage(context.Background(), owner.String(), req.CreateImageRequest{Category: "character", Prompt: prompt, AspectRatio: "1:1", IdempotencyKey: key}); err != nil {
		t.Fatalf("legacy explicit character replay: %v", err)
	}
	if *legacy.CompiledPrompt != prompt || string(legacy.EffectiveInputs) != `{"model":"old-model"}` {
		t.Fatal("legacy retry mutated frozen snapshot")
	}
}

func TestCharacterJobDTOExposesOnlyOwnedDesignInputs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode, snapshot string
		hasDesign      bool
	}{{"text_image", `{"characterDesign":{"visualStyle":"anime","mood":"warm","designFormat":"portrait"},"model":"private-model","provider":"byteplus"}`, true}, {"text_image", `{"model":"legacy"}`, false}, {"text_image", `{}`, false}, {"text_image", `{"characterDesign":null}`, false}, {"text_video", `{}`, false}} {
		job := &models.VideoGenerationJob{Prompt: "raw brief", Mode: tc.mode, EffectiveInputs: []byte(tc.snapshot)}
		dto := mapJobDTO(job)
		if (dto.CharacterDesign != nil) != tc.hasDesign {
			t.Fatalf("unexpected metadata %+v", dto)
		}
		if tc.mode == "text_image" && dto.RawPrompt != "raw brief" {
			t.Fatal("raw brief missing")
		}
		if tc.mode != "text_image" && dto.RawPrompt != "" {
			t.Fatal("video contract changed")
		}
		b, err := json.Marshal(dto)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "private-model") || strings.Contains(string(b), "byteplus") {
			t.Fatal("provider settings leaked")
		}
	}
}

type characterReferenceUploads struct {
	repo.UploadRepository
	upload *models.Upload
	err    error
	owner  string
}

func (r *characterReferenceUploads) FindByID(_ context.Context, _ string, user string) (*models.Upload, error) {
	if user != r.owner {
		return nil, repo.ErrNotFound
	}
	return r.upload, r.err
}
func TestCharacterReferenceValidationBeforeEnqueue(t *testing.T) {
	owner := uuid.NewString()
	uploadID := uuid.New()
	for _, tc := range []struct {
		name, user, mime string
		expires          time.Time
		want             error
	}{
		{"owned image", owner, "image/webp", time.Now().Add(time.Hour), nil},
		{"foreign image", uuid.NewString(), "image/png", time.Now().Add(time.Hour), ErrNotFound},
		{"expired image", owner, "image/png", time.Now().Add(-time.Hour), ErrImageExpired},
		{"audio rejected", owner, "audio/mpeg", time.Now().Add(time.Hour), ErrValidation},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &assetRepoFake{}
			uploads := &characterReferenceUploads{owner: owner, upload: &models.Upload{Base: models.Base{ID: uploadID}, MimeType: tc.mime, ExpiresAt: tc.expires}}
			f := NewStudioFacade(r, &assetJobFake{}, uploads, storage.NewMemoryClient(), "fixture", time.Hour, "snapshot-model", 3).WithReferenceAnalysisModel("vision-model")
			reference := uploadID.String()
			_, err := f.CreateImage(context.Background(), tc.user, req.CreateImageRequest{Prompt: "preserve character", AspectRatio: "3:4", ReferenceImageID: &reference, IdempotencyKey: uuid.NewString()})
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if tc.want != nil && r.queued != nil {
				t.Fatal("invalid reference enqueued")
			}
			if tc.want == nil && (r.queued == nil || r.queued.UploadID == nil || *r.queued.UploadID != uploadID || r.queued.CompiledPrompt == nil || strings.Contains(*r.queued.CompiledPrompt, "Preserve the identity and recognizable features of the supplied reference image")) {
				t.Fatal("reference identity or upload association lost")
			}
		})
	}
}

func TestProductImageSnapshotReplayAndCategory(t *testing.T) {
	t.Parallel()
	owner := uuid.NewString()
	reference := uuid.NewString()
	uploadID := uuid.MustParse(reference)
	uploads := &characterReferenceUploads{owner: owner, upload: &models.Upload{Base: models.Base{ID: uploadID}, MimeType: "image/png", ExpiresAt: time.Now().Add(time.Hour)}}
	r := &assetRepoFake{}
	f := NewStudioFacade(r, nil, uploads, nil, "fixture", time.Hour, "product-provider-model", 3).WithReferenceAnalysisModel("fixture-vision")
	input := req.CreateImageRequest{Category: "product", Prompt: "A glass bottle with its existing label", AspectRatio: "1:1", ReferenceImageID: &reference, IdempotencyKey: uuid.NewString()}
	created, err := f.CreateImage(t.Context(), owner, input)
	if err != nil {
		t.Fatal(err)
	}
	frozenSnapshot, frozenPrompt := string(r.queued.EffectiveInputs), *r.queued.CompiledPrompt
	if !strings.Contains(frozenSnapshot, `"category":"product"`) || !strings.Contains(frozenSnapshot, `"compilerVersion":"product-design-v1"`) || strings.Contains(frozenSnapshot, "characterDesign") || !strings.Contains(frozenPrompt, "Preserve the supplied reference product") || r.queued.UploadID == nil || *r.queued.UploadID != uploadID {
		t.Fatalf("product provenance missing: %s", frozenSnapshot)
	}
	same := input
	same.VisualStyle = "auto"
	same.Mood = "neutral"
	repeated, err := f.CreateImage(t.Context(), owner, same)
	if err != nil || repeated.JobID != created.JobID {
		t.Fatalf("equivalent retry: %+v %v", repeated, err)
	}
	for _, field := range []string{"category", "style", "mood", "reference", "prompt"} {
		changed := input
		switch field {
		case "category":
			changed.Category = "character"
		case "style":
			changed.VisualStyle = "anime"
		case "mood":
			changed.Mood = "warm"
		case "reference":
			changed.ReferenceImageID = nil
		case "prompt":
			changed.Prompt = "Different product"
		}
		if _, err := f.CreateImage(t.Context(), owner, changed); !errors.Is(err, ErrIdempotencyMismatch) {
			t.Errorf("changed %s accepted: %v", field, err)
		}
	}
	for _, format := range []string{"full_body", "turnaround", "portrait"} {
		bad := input
		bad.DesignFormat = format
		if _, err := f.CreateImage(t.Context(), owner, bad); !errors.Is(err, ErrValidation) {
			t.Errorf("product character framing %s accepted: %v", format, err)
		}
	}
	for _, badOptions := range []req.CreateImageRequest{{Category: "product", Prompt: "Product", AspectRatio: "1:1", VisualStyle: "unknown"}, {Category: "product", Prompt: "Product", AspectRatio: "1:1", Mood: "unknown"}} {
		isolated := &assetRepoFake{}
		isolatedFacade := NewStudioFacade(isolated, nil, nil, nil, "fixture", time.Hour, "model", 3)
		if _, err := isolatedFacade.CreateImage(t.Context(), owner, badOptions); !errors.Is(err, ErrValidation) || isolated.queued != nil {
			t.Fatalf("invalid product options enqueued: %v", err)
		}
	}
	invalid := input
	invalid.Category = "invalid"
	if _, err := f.CreateImage(t.Context(), owner, invalid); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid category accepted: %v", err)
	}
	if string(r.queued.EffectiveInputs) != frozenSnapshot || *r.queued.CompiledPrompt != frozenPrompt {
		t.Fatal("retry mutated immutable provenance")
	}
	dto := mapJobDTO(r.queued)
	if dto.ImageCategory != "product" || dto.ProductDesign == nil || dto.ProductDesign.VisualStyle != "auto" || dto.ProductDesign.Mood != "neutral" || dto.CharacterDesign != nil || dto.RawPrompt != input.Prompt {
		t.Fatalf("product job DTO %+v", dto)
	}
	legacy := mapJobDTO(&models.VideoGenerationJob{Mode: "text_image", EffectiveInputs: []byte(`{"model":"legacy"}`)})
	if legacy.ImageCategory != "character" || legacy.ProductDesign != nil {
		t.Fatalf("legacy category changed %+v", legacy)
	}
	if mapJobDTO(&models.VideoGenerationJob{Mode: "text"}).ImageCategory != "" {
		t.Fatal("video category contract changed")
	}
}
func TestProductAssetMustBePropAndRetainsJobProvenance(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	key, media := "characters/owner/job/output.png", "image"
	job := &models.VideoGenerationJob{Base: models.Base{ID: uuid.New()}, Mode: "text_image", UserID: owner, Status: models.JobStatusSucceeded, Prompt: "Original product brief", EffectiveInputs: []byte(`{"category":"product","productDesign":{"visualStyle":"auto","mood":"neutral"},"compilerVersion":"product-design-v1"}`), OutputMediaKey: &key, OutputMediaType: &media}
	frozen := string(job.EffectiveInputs)
	for _, kind := range []string{"character", "location", "costume", "style", "prop"} {
		t.Run(kind, func(t *testing.T) {
			r := &assetRepoFake{}
			f := NewStudioFacade(r, &assetJobFake{job: job}, nil, storage.NewMemoryClient(), "fixture", time.Hour, "model", 3)
			id := job.ID.String()
			got, err := f.CreateAsset(t.Context(), owner.String(), req.CreateAssetRequest{Name: "Product", AssetType: kind, Scope: "personal", JobID: &id, Prompt: "forged client prompt"})
			if kind != "prop" {
				if !errors.Is(err, ErrValidation) || r.saved != nil {
					t.Fatalf("product saved as %s: %v", kind, err)
				}
				return
			}
			if err != nil || got.AssetType != "prop" || got.JobID == nil || *got.JobID != id || got.Prompt != job.Prompt || r.saved == nil || r.saved.StorageKey == nil || *r.saved.StorageKey != key {
				t.Fatalf("product asset lost provenance: %+v %v", got, err)
			}
		})
	}
	if string(job.EffectiveInputs) != frozen {
		t.Fatal("asset save mutated snapshot")
	}
}

func TestImageCapabilitiesAdvertiseCategoriesRegardlessProviderReadiness(t *testing.T) {
	t.Parallel()
	for _, model := range []string{"", "activated-image-model"} {
		facade := NewStudioFacade(nil, nil, nil, nil, "fixture", time.Hour, model, 3)
		capabilities := facade.Capabilities()
		encoded, err := json.Marshal(capabilities)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields["categories"]) != `["character","product"]` {
			t.Errorf("category support missing with provider readiness %t: %s", model != "", encoded)
		}
		if capabilities.Enabled != (model != "") || capabilities.SupportsReferenceImage != (model != "") || capabilities.Model != "character-image" || len(capabilities.AspectRatios) != 5 || (capabilities.UnavailableReason != nil) != (model == "") {
			t.Fatalf("existing capability behavior changed: %+v", capabilities)
		}
		for _, name := range []string{"enabled", "model", "aspectRatios", "supportsReferenceImage", "unavailableReason"} {
			if _, ok := fields[name]; !ok {
				t.Errorf("legacy capability field omitted: %s", name)
			}
		}
	}
}

func TestReferenceReadinessAndFrozenIdempotentReplay(t *testing.T) {
	owner, id := uuid.New(), uuid.New()
	r := &assetRepoFake{}
	uploads := &characterReferenceUploads{owner: owner.String(), upload: &models.Upload{Base: models.Base{ID: id}, MimeType: "image/png", ExpiresAt: time.Now().Add(time.Hour)}}
	f := NewStudioFacade(r, nil, uploads, nil, "fixture", time.Hour, "image-fixture", 3)
	ref := id.String()
	in := req.CreateImageRequest{Prompt: "new fictional explorer", AspectRatio: "1:1", ReferenceImageID: &ref, IdempotencyKey: uuid.NewString()}
	cap := f.Capabilities()
	if !cap.SupportsCharacterReferenceImage || cap.CharacterReferenceMode != "image_to_image" || cap.CharacterReferenceUnavailableReason != nil || !cap.SupportsReferenceImage {
		t.Fatalf("wrong capability: %+v", cap)
	}
	if _, err := f.CreateImage(t.Context(), owner.String(), in); err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(r.queued.EffectiveInputs, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot["imageGenerationMode"] != "image_to_image" || snapshot["referenceStrategy"] != nil || snapshot["referenceAnalysisModel"] != nil || snapshot["compilerVersion"] != characterdesign.CompilerVersion {
		t.Fatalf("missing immutable inputs: %v", snapshot)
	}
	if r.queued.CompiledPrompt == nil || !strings.Contains(*r.queued.CompiledPrompt, "synthetic, fictional") || !strings.Contains(*r.queued.CompiledPrompt, "face geometry") {
		t.Fatal("synthetic reference prompt missing")
	}
	old := r.queued
	initial := string(old.EffectiveInputs)
	replay := NewStudioFacade(r, &assetJobFake{job: old}, nil, nil, "fixture", time.Hour, "", 3)
	again, err := replay.CreateImage(t.Context(), owner.String(), in)
	if err != nil || again.JobID != old.ID.String() || string(old.EffectiveInputs) != initial {
		t.Fatal("replay changed after config removal", err)
	}
	changed := in
	changed.Prompt = "different"
	if _, err := replay.CreateImage(t.Context(), owner.String(), changed); !errors.Is(err, ErrIdempotencyMismatch) {
		t.Fatal("replay mismatch accepted", err)
	}
}
