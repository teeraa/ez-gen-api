package facade_open_api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/google/uuid"
)

type unavailablePipelineImages struct{ *pipelineImage }

func (i unavailablePipelineImages) Capabilities() dto.ImageCapabilitiesDTO {
	return dto.ImageCapabilitiesDTO{}
}
func (i unavailablePipelineImages) PipelineImageModel() string { return "" }

func TestPipelineTextSourceAndValidation(t *testing.T) {
	f, m, images, video, user, in := pipelineFixture(t)
	in.ReferenceImageID = ""
	out, err := f.Create(context.Background(), user, in)
	if err != nil || out.SourceMode != "text" {
		t.Fatalf("create %v %v", out, err)
	}
	runPipelineTicks(t, f, 8)
	if images.calls != 1 || images.seen.ReferenceImageID != nil || video.calls != 1 || m.p.Status != "succeeded" {
		t.Fatalf("wrong text flow: image=%d video=%d pipeline=%+v", images.calls, video.calls, m.p)
	}
	f, _, _, _, user, in = pipelineFixture(t)
	in.ReferenceImageID = ""
	in.CharacterPrompt = " "
	if _, err = f.Create(context.Background(), user, in); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing text prompt %v", err)
	}
	f, _, _, _, user, in = pipelineFixture(t)
	in.CharacterAssetID = uuid.NewString()
	if _, err = f.Create(context.Background(), user, in); !errors.Is(err, ErrValidation) {
		t.Fatalf("both references %v", err)
	}
}

func seedLibraryCharacter(m *pipelineMemory, user string) uuid.UUID {
	uid := uuid.MustParse(user)
	id := uuid.New()
	job := uuid.New()
	key := "characters/" + job.String() + ".png"
	media := "image"
	m.js[job.String()] = &models.VideoGenerationJob{Base: models.Base{ID: job}, UserID: uid, Status: models.JobStatusSucceeded, Mode: "text_image", OutputMediaKey: &key, OutputMediaType: &media, EffectiveInputs: []byte(`{"category":"character"}`)}
	m.as[id.String()] = &models.LibraryAsset{Base: models.Base{ID: id}, UserID: uid, AssetType: "character", JobID: &job, StorageKey: &key, MediaType: &media}
	return id
}
func TestPipelineLibrarySkipsImageAndReplaySurvivesDeletion(t *testing.T) {
	f, m, images, video, user, in := pipelineFixture(t)
	id := seedLibraryCharacter(m, user)
	in.ReferenceImageID = ""
	in.CharacterAssetID = id.String()
	in.CharacterPrompt = ""
	f.images = unavailablePipelineImages{images}
	caps := f.Capabilities()
	if caps.Enabled || caps.TextEnabled || !caps.LibraryEnabled || len(caps.LibraryDurations) == 0 {
		t.Fatalf("capabilities %+v", caps)
	}
	out, err := f.Create(context.Background(), user, in)
	if err != nil || out.SourceMode != "library" || out.Stage != "video" || out.CharacterJobID != nil {
		t.Fatalf("library %v %v", out, err)
	}
	runPipelineTicks(t, f, 4)
	if images.calls != 0 || video.calls != 1 || m.p.Status != "succeeded" || *video.seen.ReferenceAssetID != id.String() {
		t.Fatal("library did not reuse asset")
	}
	delete(m.as, id.String())
	f.videoEnabled = false
	if _, err = f.Create(context.Background(), user, in); err != nil {
		t.Fatalf("deleted replay %v", err)
	}
}
func TestPipelineLibraryRejectsInvalidProvenance(t *testing.T) {
	for _, mutation := range []string{"owner", "type", "key", "job", "source", "media", "upload", "deleted"} {
		t.Run(mutation, func(t *testing.T) {
			f, m, _, _, user, in := pipelineFixture(t)
			id := seedLibraryCharacter(m, user)
			a := m.as[id.String()]
			switch mutation {
			case "owner":
				a.UserID = uuid.New()
			case "type":
				a.AssetType = "product"
			case "key":
				s := "https://example.com/image"
				a.StorageKey = &s
			case "job":
				m.js[a.JobID.String()].Status = models.JobStatusFailed
			case "source":
				m.js[a.JobID.String()].EffectiveInputs = []byte(`{"category":"product"}`)
			case "media":
				s := "video"
				a.MediaType = &s
			case "upload":
				u := uuid.New()
				a.UploadID = &u
			case "deleted":
				delete(m.as, id.String())
			}
			in.ReferenceImageID = ""
			in.CharacterAssetID = id.String()
			if _, err := f.Create(context.Background(), user, in); err == nil {
				t.Fatal("accepted invalid asset")
			}
		})
	}
}
func TestPipelineLegacyHashAndSnapshotReplay(t *testing.T) {
	f, m, _, _, user, in := pipelineFixture(t)
	normalized := normalizePipeline(in)
	// This literal is the previous request serialization, with no newly introduced fields.
	dialogue, _ := json.Marshal(in.Dialogue)
	legacy := `{"referenceImageId":"` + in.ReferenceImageID + `","characterPrompt":"` + in.CharacterPrompt + `","scenePrompt":"` + in.ScenePrompt + `","dialogue":` + string(dialogue) + `,"visualStyle":"cinematic_realistic","aspectRatio":"9:16","durationSeconds":5,"modelId":"violet-motion-2","quality":"standard","idempotencyKey":"` + in.IdempotencyKey + `"}`
	current, _ := json.Marshal(normalized)
	if string(current) != legacy {
		t.Fatalf("legacy serialization changed\n%s\n%s", current, legacy)
	}
	sum := sha256.Sum256([]byte(legacy))
	snapshot := []byte(`{"request":` + legacy + `,"imageModel":"fixture-image","videoModel":{}}`)
	m.p = &models.StudioPipeline{Base: models.Base{ID: uuid.New()}, UserID: uuid.MustParse(user), IdempotencyKey: in.IdempotencyKey, RequestHash: hex.EncodeToString(sum[:]), Inputs: snapshot, Status: "queued", Stage: "image"}
	m.upload.ExpiresAt = time.Now().Add(-time.Hour)
	f.videoEnabled = false
	out, err := f.Create(context.Background(), user, in)
	if err != nil || out.SourceMode != "upload" {
		t.Fatalf("legacy replay %v %v", out, err)
	}
}

func TestPipelineLibraryDeletedBeforeWorkerFailsWithoutGeneration(t *testing.T) {
	f, m, images, _, user, in := pipelineFixture(t)
	id := seedLibraryCharacter(m, user)
	in.ReferenceImageID = ""
	in.CharacterAssetID = id.String()
	if _, err := f.Create(context.Background(), user, in); err != nil {
		t.Fatal(err)
	}
	reg, _ := modelark.NewRegistry("")
	ark := &pipelineRecordingArk{}
	actual := NewJobFacade(pipelineJobStore{m: m}, nil, nil, nil, nil, nil, ark, reg, storage.NewMemoryClient(), nil, 5)
	ConfigureJobPipeline(actual, m)
	f.jobs = actual
	delete(m.as, id.String())
	runPipelineTicks(t, f, 1)
	if m.p.Status != "failed" || m.p.FailureCode == nil || *m.p.FailureCode != "INPUT_UNAVAILABLE" || images.calls != 0 || ark.calls != 0 {
		t.Fatalf("deleted source %+v images=%d calls=%d", m.p, images.calls, ark.calls)
	}
}
