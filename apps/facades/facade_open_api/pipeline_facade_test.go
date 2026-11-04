package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/characterdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/promptcompiler"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/google/uuid"
	"strings"
	"sync"
	"testing"
	"time"
)

type pipelineMemory struct {
	repo.StudioRepository
	repo.UploadRepository
	repo.JobRepository
	mu        sync.Mutex
	p         *models.StudioPipeline
	js        map[string]*models.VideoGenerationJob
	as        map[string]*models.LibraryAsset
	upload    *models.Upload
	saveFails bool
}

func clonePipeline(p *models.StudioPipeline) *models.StudioPipeline {
	b, _ := json.Marshal(p)
	var q models.StudioPipeline
	_ = json.Unmarshal(b, &q)
	return &q
}
func (m *pipelineMemory) Create(_ context.Context, p *models.StudioPipeline) (*models.StudioPipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.p != nil {
		return clonePipeline(m.p), nil
	}
	p.ID = uuid.New()
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt
	m.p = clonePipeline(p)
	return clonePipeline(p), nil
}
func (m *pipelineMemory) ByKey(_ context.Context, key, user string) (*models.StudioPipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.p != nil && m.p.IdempotencyKey == key && m.p.UserID.String() == user {
		return clonePipeline(m.p), nil
	}
	return nil, repo.ErrNotFound
}
func (m *pipelineMemory) Get(_ context.Context, id, user string) (*models.StudioPipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.p != nil && m.p.ID.String() == id && m.p.UserID.String() == user {
		return clonePipeline(m.p), nil
	}
	return nil, repo.ErrNotFound
}
func (m *pipelineMemory) List(_ context.Context, user string, page, limit int) ([]models.StudioPipeline, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.p != nil && m.p.UserID.String() == user {
		return []models.StudioPipeline{*clonePipeline(m.p)}, 1, nil
	}
	return []models.StudioPipeline{}, 0, nil
}
func (m *pipelineMemory) Claim(_ context.Context) (*models.StudioPipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.p == nil || (m.p.Status != "queued" && m.p.Status != "processing") || (m.p.LeaseUntil != nil && m.p.LeaseUntil.After(time.Now())) {
		return nil, repo.ErrNotFound
	}
	id := uuid.New()
	until := time.Now().Add(10 * time.Minute)
	m.p.LeaseToken = &id
	m.p.LeaseUntil = &until
	m.p.Status = "processing"
	return clonePipeline(m.p), nil
}
func (m *pipelineMemory) Advance(_ context.Context, p *models.StudioPipeline) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.p.LeaseToken == nil || p.LeaseToken == nil || *m.p.LeaseToken != *p.LeaseToken {
		return repo.ErrConflict
	}
	m.p = clonePipeline(p)
	m.p.LeaseUntil = nil
	m.p.LeaseToken = nil
	return nil
}
func (m *pipelineMemory) Retry(_ context.Context, id, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.p == nil || m.p.ID.String() != id || m.p.UserID.String() != user {
		return repo.ErrNotFound
	}
	if m.p.Status == "queued" || m.p.Status == "processing" {
		return nil
	}
	if m.p.Status != "failed" {
		return repo.ErrConflict
	}
	if m.p.Stage == "video" {
		m.p.VideoAttempt++
		m.p.VideoJobID = nil
	} else if m.p.Stage != "save_character" && m.p.Stage != "save_video" {
		return repo.ErrConflict
	}
	m.p.Status = "queued"
	m.p.FailureCode = nil
	m.p.FailureMessage = nil
	return nil
}
func (m *pipelineMemory) InsertAsset(_ context.Context, a *models.LibraryAsset) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveFails {
		return errors.New("fixture save failure")
	}
	m.as[a.ID.String()] = a
	return nil
}
func (m *pipelineMemory) Asset(_ context.Context, id, user string) (*models.LibraryAsset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.as[id]
	if a == nil || a.UserID.String() != user {
		return nil, repo.ErrNotFound
	}
	return a, nil
}
func (m *pipelineMemory) FindByID(_ context.Context, id, user string) (*models.VideoGenerationJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.js[id]
	if j == nil || j.UserID.String() != user {
		return nil, repo.ErrNotFound
	}
	copy := *j
	return &copy, nil
}
func (m *pipelineMemory) FindByIdempotencyKey(_ context.Context, key, user string) (*models.VideoGenerationJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.js {
		if j.UserID.String() == user && j.IdempotencyKey != nil && *j.IdempotencyKey == key {
			copy := *j
			return &copy, nil
		}
	}
	return nil, repo.ErrNotFound
}
func (m *pipelineMemory) FindUpload(_ context.Context, id, user string) (*models.Upload, error) {
	if m.upload == nil || m.upload.ID.String() != id || m.upload.UserID.String() != user {
		return nil, repo.ErrNotFound
	}
	return m.upload, nil
}

type pipelineUpload struct {
	repo.UploadRepository
	m *pipelineMemory
}

func (u pipelineUpload) FindByID(c context.Context, id, user string) (*models.Upload, error) {
	return u.m.FindUpload(c, id, user)
}

type pipelineImage struct {
	m                *pipelineMemory
	calls            int
	seen             req.CreateImageRequest
	renderingVersion string
}

func (i *pipelineImage) Capabilities() dto.ImageCapabilitiesDTO {
	return dto.ImageCapabilitiesDTO{Enabled: true, SupportsCharacterReferenceImage: true, AspectRatios: []string{"1:1", "3:4", "4:3", "16:9", "9:16"}}
}
func (i *pipelineImage) PipelineImageModel() string { return "fixture-image" }
func (i *pipelineImage) CreateImage(c context.Context, u string, in req.CreateImageRequest) (*dto.CreateJobResponseDTO, error) {
	return i.CreatePipelineImage(c, u, in, "fixture-image", "")
}
func (i *pipelineImage) CreatePipelineImage(c context.Context, u string, in req.CreateImageRequest, model string, renderingVersion string) (*dto.CreateJobResponseDTO, error) {
	old, e := i.m.FindByIdempotencyKey(c, in.IdempotencyKey, u)
	if e == nil {
		return mapCreateJobResponse(old), nil
	}
	i.calls++
	i.seen = in
	i.renderingVersion = renderingVersion
	uid, _ := uuid.Parse(u)
	id := uuid.New()
	key := "characters/" + id.String() + ".png"
	kind := "image"
	var upload *uuid.UUID
	if in.ReferenceImageID != nil {
		id, _ := uuid.Parse(*in.ReferenceImageID)
		upload = &id
	}
	j := &models.VideoGenerationJob{Base: models.Base{ID: id}, UserID: uid, IdempotencyKey: &in.IdempotencyKey, UploadID: upload, Status: models.JobStatusSucceeded, Mode: "text_image", Prompt: in.Prompt, OutputMediaKey: &key, OutputMediaType: &kind, EffectiveInputs: []byte(`{"category":"character"}`)}
	i.m.mu.Lock()
	i.m.js[id.String()] = j
	i.m.mu.Unlock()
	return mapCreateJobResponse(j), nil
}

type pipelineVideo struct {
	JobFacade
	m     *pipelineMemory
	calls int
	seen  CreateJobInput
}

func (v *pipelineVideo) CreateJob(c context.Context, u string, in CreateJobInput) (*dto.CreateJobResponseDTO, error) {
	old, e := v.m.FindByIdempotencyKey(c, *in.IdempotencyKey, u)
	if e == nil {
		return mapCreateJobResponse(old), nil
	}
	v.calls++
	v.seen = in
	uid, _ := uuid.Parse(u)
	id := uuid.New()
	key := "videos/" + id.String() + ".mp4"
	kind := "video"
	j := &models.VideoGenerationJob{Base: models.Base{ID: id}, UserID: uid, IdempotencyKey: in.IdempotencyKey, Status: models.JobStatusSucceeded, IsDraft: in.Draft, Mode: in.Mode, Prompt: in.Prompt, OutputMediaKey: &key, OutputMediaType: &kind}
	v.m.mu.Lock()
	v.m.js[id.String()] = j
	v.m.mu.Unlock()
	return mapCreateJobResponse(j), nil
}
func pipelineFixture(t *testing.T) (*PipelineFacade, *pipelineMemory, *pipelineImage, *pipelineVideo, string, req.CreatePipelineRequest) {
	t.Helper()
	user := uuid.New()
	upload := uuid.New()
	m := &pipelineMemory{js: map[string]*models.VideoGenerationJob{}, as: map[string]*models.LibraryAsset{}, upload: &models.Upload{Base: models.Base{ID: upload}, UserID: user, MimeType: "image/png", ExpiresAt: time.Now().Add(time.Hour)}}
	images := &pipelineImage{m: m}
	videos := &pipelineVideo{m: m}
	reg, _ := modelark.NewRegistry("")
	f := NewPipelineFacade(m, images, videos, m, m, pipelineUpload{m: m}, reg, storage.NewMemoryClient(), true)
	in := req.CreatePipelineRequest{ReferenceImageID: upload.String(), CharacterPrompt: "ตัวละครสังเคราะห์คงเค้าเดิม", ScenePrompt: "เดินกลางสวน แสงเย็น", Dialogue: "สวัสดี \"ทุกคน\"\nยินดีที่ได้พบ", IdempotencyKey: uuid.NewString()}
	return f, m, images, videos, user.String(), in
}
func runPipelineTicks(t *testing.T, f *PipelineFacade, n int) {
	t.Helper()
	for range n {
		if e := f.RunOnce(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
}
func TestPipelineFullFlowReplayRestartConcurrency(t *testing.T) {
	t.Parallel()
	f, m, images, videos, user, in := pipelineFixture(t)
	c := context.Background()
	first, e := f.Create(c, user, in)
	if e != nil {
		t.Fatal(e)
	}
	again, e := f.Create(c, user, in)
	if e != nil || again.ID != first.ID {
		t.Fatalf("replay %+v %v", again, e)
	}
	changed := in
	changed.Dialogue += "อื่น"
	if _, e = f.Create(c, user, changed); !errors.Is(e, ErrIdempotencyMismatch) {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _ = f.RunOnce(c) }()
	}
	wg.Wait()
	runPipelineTicks(t, f, 8)
	d, e := f.Get(c, first.ID, user)
	if e != nil || d.Status != "succeeded" || d.Stage != "done" || len(m.as) != 2 || images.calls != 1 || videos.calls != 1 {
		t.Fatalf("flow %+v %v image=%d video=%d assets=%d", d, e, images.calls, videos.calls, len(m.as))
	}
	if videos.seen.UploadID != nil || videos.seen.ReferenceAssetID == nil || *videos.seen.ReferenceAssetID != *d.CharacterAssetID || videos.seen.Mode != "image_image" || videos.seen.Dialogue != in.Dialogue || videos.seen.PinnedModel == nil || images.seen.DesignFormat != "full_body" || *images.seen.ReferenceImageID != in.ReferenceImageID {
		t.Fatal("source boundary or exact dialogue lost")
	}
	if d.Dialogue != in.Dialogue || d.CharacterMediaURL == nil || d.VideoMediaURL == nil {
		t.Fatal("restore snapshot/media missing")
	}
	if _, e = f.Get(c, first.ID, uuid.NewString()); !errors.Is(e, ErrNotFound) {
		t.Fatal("owner isolation", e)
	}
	// A new facade models a restarted worker using the same durable store.
	restarted := *f
	runPipelineTicks(t, &restarted, 3)
	if images.calls != 1 || videos.calls != 1 {
		t.Fatal("restart duplicated generation")
	}
}
func TestPipelineAssetSaveRetryRetainsGeneration(t *testing.T) {
	t.Parallel()
	f, m, images, videos, user, in := pipelineFixture(t)
	d, e := f.Create(context.Background(), user, in)
	if e != nil {
		t.Fatal(e)
	}
	runPipelineTicks(t, f, 2)
	m.saveFails = true
	runPipelineTicks(t, f, 1)
	d, e = f.Get(context.Background(), d.ID, user)
	if e != nil || d.Status != "failed" || !d.CanRetry {
		t.Fatalf("save %+v %v", d, e)
	}
	m.saveFails = false
	if _, e = f.Retry(context.Background(), d.ID, user); e != nil {
		t.Fatal(e)
	}
	runPipelineTicks(t, f, 8)
	if images.calls != 1 || videos.calls != 1 || len(m.as) != 2 {
		t.Fatal("save retry regenerated or duplicated")
	}
}
func TestPipelineVideoFailureAndUnknownOutcomes(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"PROVIDER_INVALID_INPUT", "SUBMISSION_INTERRUPTED", "WORKER_INTERRUPTED", "PROVIDER_TIMEOUT", "PROVIDER_ERROR"} {
		t.Run(code, func(t *testing.T) {
			f, m, images, videos, user, in := pipelineFixture(t)
			d, e := f.Create(context.Background(), user, in)
			if e != nil {
				t.Fatal(e)
			}
			runPipelineTicks(t, f, 4)
			j := m.js[m.p.VideoJobID.String()]
			j.Status = models.JobStatusFailed
			j.FailureCode = &code
			runPipelineTicks(t, f, 1)
			d, e = f.Get(context.Background(), d.ID, user)
			if e != nil {
				t.Fatal(e)
			}
			if len(m.as) != 1 || d.CharacterAssetID == nil {
				t.Fatal("lost saved character")
			}
			if code != "PROVIDER_INVALID_INPUT" {
				if d.Status != "needs_attention" || d.CanRetry {
					t.Fatalf("unsafe %+v", d)
				}
				if _, e = f.Retry(context.Background(), d.ID, user); !errors.Is(e, ErrConflict) {
					t.Fatal("unknown retry permitted", e)
				}
				return
			}
			if !d.CanRetry {
				t.Fatal("known failure not retryable")
			}
			if _, e = f.Retry(context.Background(), d.ID, user); e != nil {
				t.Fatal(e)
			}
			if _, e = f.Retry(context.Background(), d.ID, user); e != nil {
				t.Fatal(e)
			}
			runPipelineTicks(t, f, 5)
			if images.calls != 1 || videos.calls != 2 || m.p.VideoAttempt != 1 {
				t.Fatal("retry duplicated", images.calls, videos.calls, m.p.VideoAttempt)
			}
		})
	}
}
func TestPipelinePrevalidationAndImmutableModel(t *testing.T) {
	t.Parallel()
	f, m, _, _, user, in := pipelineFixture(t)
	bad := in
	bad.VisualStyle = "invalid"
	if _, e := f.Create(context.Background(), user, bad); !errors.Is(e, ErrValidation) {
		t.Fatal("invalid style persisted", e)
	}
	bad = in
	bad.ScenePrompt = fmt.Sprintf("%02000d", 0)
	bad.Dialogue = "ข้อความ"
	if _, e := f.Create(context.Background(), user, bad); !errors.Is(e, ErrValidation) {
		t.Fatal("scene would truncate", e)
	}
	d, e := f.Create(context.Background(), user, in)
	if e != nil {
		t.Fatal(e)
	}
	var frozen pipelineSnapshot
	_ = json.Unmarshal(m.p.Inputs, &frozen)
	if frozen.VideoModel.ProviderModelID == "" || frozen.ImageModel == "" {
		t.Fatal("models not pinned")
	}
	m.upload.ExpiresAt = time.Now().Add(-time.Hour)
	f.videoEnabled = false
	if _, e = f.Create(context.Background(), user, in); e != nil {
		t.Fatal("replay depends on current capabilities", e)
	}
	if _, e = f.Get(context.Background(), d.ID, uuid.NewString()); !errors.Is(e, ErrNotFound) {
		t.Fatal(e)
	}
}

type pipelineJobStore struct {
	repo.JobRepository
	m *pipelineMemory
}

func (s pipelineJobStore) Create(_ context.Context, j *models.VideoGenerationJob) error {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	j.ID = uuid.New()
	s.m.js[j.ID.String()] = j
	return nil
}
func (s pipelineJobStore) FindByID(c context.Context, id, user string) (*models.VideoGenerationJob, error) {
	return s.m.FindByID(c, id, user)
}
func (s pipelineJobStore) FindByIdempotencyKey(c context.Context, key, user string) (*models.VideoGenerationJob, error) {
	return s.m.FindByIdempotencyKey(c, key, user)
}
func (s pipelineJobStore) CountActiveByUser(context.Context, string) (int64, error) { return 0, nil }
func (s pipelineJobStore) UpdateStatus(_ context.Context, id string, status models.JobStatus, fields map[string]any) error {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	j := s.m.js[id]
	j.Status = status
	if task, ok := fields["provider_task_id"].(string); ok {
		j.ProviderTaskID = &task
	}
	return nil
}

type pipelineRecordingArk struct {
	modelark.Client
	seen  modelark.CreateTaskRequest
	calls int
}

func (a *pipelineRecordingArk) CreateTask(_ context.Context, in modelark.CreateTaskRequest) (string, error) {
	a.calls++
	a.seen = in
	return "fixture-provider-task", nil
}
func TestPipelineGeneratedAssetActualJobFacadeBoundary(t *testing.T) {
	t.Parallel()
	var v2VideoPrompt string
	for _, version := range []string{"", characterdesign.PipelineCompilerVersion, characterdesign.PipelineRealisticCompilerVersion, characterdesign.PipelineSoftRealisticCompilerVersion, characterdesign.PipelineSemiRealisticCompilerVersion, characterdesign.PipelinePolished3DCompilerVersion, characterdesign.PipelineStylized3DCompilerVersion, promptcompiler.PipelineCinematicVideoVersion, promptcompiler.PipelineFilmedHumanVideoVersion} {
		t.Run(version, func(t *testing.T) {
			f, m, _, _, user, in := pipelineFixture(t)
			c := context.Background()
			if _, e := f.Create(c, user, in); e != nil {
				t.Fatal(e)
			}
			runPipelineTicks(t, f, 3)
			reg, _ := modelark.NewRegistry("")
			ark := &pipelineRecordingArk{}
			jobs := pipelineJobStore{m: m}
			actual := NewJobFacade(jobs, nil, nil, nil, nil, nil, ark, reg, storage.NewMemoryClient(), nil, 5)
			ConfigureJobPipeline(actual, m)
			asset := m.p.CharacterAssetID.String()
			key := uuid.NewString()
			input := CreateJobInput{pipelineRenderingVersion: version, ReferenceAssetID: &asset, Dialogue: in.Dialogue, Prompt: in.ScenePrompt, Mode: "image_image", ModelID: "violet-motion-2", AspectRatio: "9:16", DurationSeconds: 5, Quality: "standard", IdempotencyKey: &key}
			out, e := actual.CreateJob(c, user, input)
			if e != nil {
				t.Fatal(e)
			}
			if len(ark.seen.Content) != 2 || ark.seen.Content[1].Role != modelark.RoleReferenceImage || !ark.seen.GenerateAudio {
				t.Fatal("SDK request missing reference/audio")
			}
			generated := m.as[asset]
			if !strings.Contains(ark.seen.Content[1].URL, *generated.StorageKey) || strings.Contains(ark.seen.Content[1].URL, in.ReferenceImageID) {
				t.Fatal("original source reached video")
			}
			job := m.js[out.JobID]
			if job.UploadID != nil {
				t.Fatal("video persisted source upload")
			}
			var snap struct {
				Dialogue                 string
				PipelineRenderingVersion string
			}
			_ = json.Unmarshal(job.EffectiveInputs, &snap)
			if snap.Dialogue != in.Dialogue || snap.PipelineRenderingVersion != version {
				t.Fatal("snapshot dialogue mismatch")
			}
			text := ark.seen.Content[0].Text
			if version == characterdesign.PipelineRealisticCompilerVersion {
				v2VideoPrompt = text
			}
			if (version == characterdesign.PipelineSoftRealisticCompilerVersion || version == characterdesign.PipelineSemiRealisticCompilerVersion || version == characterdesign.PipelinePolished3DCompilerVersion || version == characterdesign.PipelineStylized3DCompilerVersion) && text != v2VideoPrompt {
				t.Fatal("new image version changed v2 video rendering instructions")
			}

			switch version {
			case promptcompiler.PipelineFilmedHumanVideoVersion:
				for _, want := range []string{"casting/design guide for face/body geometry, haircut and outfit only", "no beautification or identity drift", "Exclude its shading, CG materials, drawn skin, pose and background", "Not a texture/style/start frame", "Filmed live-action human actor look", "skin with subtle texture and translucency", "individual hair and real cloth", "Natural eyes, blinks", "grounded motion", "Motivated practical lighting", "subtle shutter motion blur", "follow scene framing"} {
					if !strings.Contains(text, want) {
						t.Errorf("filmed-human provider prompt missing %q", want)
					}
				}
				for _, forbidden := range []string{"use only the subject appearance", "do not reinterpret it as live action", "Preserve the selected visual style", "realistic cinematic CG", "8K"} {
					if strings.Contains(text, forbidden) {
						t.Errorf("contradictory filmed-human control %q", forbidden)
					}
				}
			case promptcompiler.PipelineCinematicVideoVersion:
				for _, want := range []string{"identity only, not style", "Photoreal live-action film humans", "skin microtexture/imperfections", "grounded weight/motion", "expressions, blinks, natural lip sync", "Turn matte CG to human materials", "Motivated lighting", "natural shadows, optical depth, subtle motion blur", "follow scene camera/framing"} {
					if !strings.Contains(text, want) {
						t.Errorf("cinematic provider prompt missing %q", want)
					}
				}
				for _, forbidden := range []string{"do not reinterpret it as live action", "Preserve the selected visual style", "realistic cinematic CG", "8K"} {
					if strings.Contains(text, forbidden) {
						t.Errorf("contradictory cinematic control %q", forbidden)
					}
				}
				if text == v2VideoPrompt {
					t.Fatal("v7 did not change video rendering")
				}

			case characterdesign.PipelineRealisticCompilerVersion, characterdesign.PipelineSoftRealisticCompilerVersion, characterdesign.PipelineSemiRealisticCompilerVersion, characterdesign.PipelinePolished3DCompilerVersion, characterdesign.PipelineStylized3DCompilerVersion:
				for _, want := range []string{"photorealistic live-action human", "realistic skin texture", "natural motion", "Preserve similar facial structure"} {
					if !strings.Contains(text, want) {
						t.Errorf("provider prompt missing %q", want)
					}
				}
				for _, forbidden := range []string{"do not reinterpret it as live action", "realistic cinematic CG", "Preserve the selected visual style"} {
					if strings.Contains(text, forbidden) {
						t.Errorf("contradictory provider prompt: %q", forbidden)
					}
				}
			case characterdesign.PipelineCompilerVersion:
				if !strings.Contains(text, "realistic cinematic CG") {
					t.Fatal("frozen v1 changed")
				}
			default:
				if !strings.Contains(text, "do not reinterpret it as live action") {
					t.Fatal("legacy nonpipeline controls changed")
				}
			}
			if _, e = actual.CreateJob(c, user, input); e != nil || ark.calls != 1 {
				t.Fatal("paid replay", e)
			}
			bad := input
			bad.UploadID = &in.ReferenceImageID
			bad.IdempotencyKey = nil
			if _, e = actual.CreateJob(c, user, bad); !errors.Is(e, ErrValidation) {
				t.Fatal("source bypass", e)
			}
			if _, e = actual.CreateJob(c, uuid.NewString(), input); !errors.Is(e, ErrNotFound) {
				t.Fatal("foreign asset bypass", e)
			}
		})
	}
}

func TestPipelineCapabilitiesIntersectBothProvidersAndRequestLimits(t *testing.T) {
	t.Parallel()
	for _, model := range []string{"dreamina-seedance-2-0-260128", "dreamina-seedance-2-5-260628"} {
		t.Run(model, func(t *testing.T) {
			f, m, _, _, user, in := pipelineFixture(t)
			registry, e := modelark.NewRegistry(model)
			if e != nil {
				t.Fatal(e)
			}
			f.registry = registry
			cap := f.Capabilities()
			if !cap.Enabled {
				t.Fatal("supported model unavailable")
			}
			cfg, _ := registry.Lookup("violet-motion-2")
			for _, ratio := range cap.AspectRatios {
				for _, duration := range cap.Durations {
					if !pipelineOutputSupported(f.images.Capabilities(), cfg, ratio, duration) {
						t.Fatalf("advertised impossible pair %s/%d", ratio, duration)
					}
				}
			}
			for _, ratio := range cap.AspectRatios {
				if ratio == "21:9" {
					t.Fatal("image unsupported ratio advertised")
				}
			}
			for _, duration := range cap.Durations {
				if duration < 4 || duration > 15 {
					t.Fatal("request rejects advertised duration", duration)
				}
			}
			bad := in
			bad.AspectRatio = "21:9"
			if _, e = f.Create(context.Background(), user, bad); !errors.Is(e, ErrModelInvalid) {
				t.Fatal("unsupported image ratio accepted", e)
			}
			if m.p != nil {
				t.Fatal("unsupported image ratio persisted")
			}
			bad = in
			bad.DurationSeconds = 16
			if _, e = f.Create(context.Background(), user, bad); !errors.Is(e, ErrModelInvalid) {
				t.Fatal("duration outside request accepted", e)
			}
			if m.p != nil {
				t.Fatal("unsupported duration persisted")
			}
			if _, e = f.Create(context.Background(), user, in); e != nil {
				t.Fatal("supported pair rejected", e)
			}
		})
	}
}
