package crons

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	facade "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/characterdesign"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/google/uuid"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type workerRepo struct {
	repo.StudioRepository
	job              *models.VideoGenerationJob
	status           models.JobStatus
	fields           map[string]any
	preparedPrompt   string
	preparedSnapshot []byte
	prepareErr       error
}

func (r *workerRepo) ClaimImage(context.Context) (*models.VideoGenerationJob, error) {
	return r.job, nil
}
func (r *workerRepo) PrepareImage(_ context.Context, _ string, prompt string, snapshot []byte) error {
	r.preparedPrompt = prompt
	r.preparedSnapshot = snapshot
	return r.prepareErr
}
func (r *workerRepo) FinishImage(_ context.Context, _ string, s models.JobStatus, m map[string]any) error {
	r.status = s
	r.fields = m
	return nil
}

type imageGenerator struct {
	data    []byte
	err     error
	calls   int
	request modelark.ImageRequest
}

func (g *imageGenerator) GenerateImage(_ context.Context, in modelark.ImageRequest) ([]byte, error) {
	g.calls++
	g.request = in
	if in.Model != "snapshot-model" {
		return nil, errors.New("snapshot model lost")
	}
	return g.data, g.err
}
func fixturePNG() []byte {
	var b bytes.Buffer
	im := image.NewRGBA(image.Rect(0, 0, 2, 2))
	im.Set(0, 0, color.RGBA{R: 255, A: 255})
	png.Encode(&b, im)
	return b.Bytes()
}
func TestWorkerValidatesAndPersistsBeforeSuccess(t *testing.T) {
	for _, tc := range []struct {
		name       string
		data       []byte
		storageErr bool
		want       models.JobStatus
	}{{"valid", fixturePNG(), false, models.JobStatusSucceeded}, {"invalid bytes", []byte("not an image"), false, models.JobStatusFailed}, {"truncated", fixturePNG()[:30], false, models.JobStatusFailed}, {"storage down", fixturePNG(), true, models.JobStatusFailed}} {
		t.Run(tc.name, func(t *testing.T) {
			r := &workerRepo{job: &models.VideoGenerationJob{Base: models.Base{ID: uuid.New()}, UserID: uuid.New(), Prompt: "character", AspectRatio: "1:1", EffectiveInputs: []byte(`{"model":"snapshot-model","size":"2048x2048"}`)}}
			g := &imageGenerator{data: tc.data}
			s := storage.NewMemoryClient()
			if tc.storageErr {
				s.UploadErr = errors.New("down")
			}
			w := NewImageWorker(r, nil, g, s)
			if e := w.RunOnce(context.Background()); e != nil {
				t.Fatal(e)
			}
			if r.status != tc.want || g.calls != 1 {
				t.Fatalf("status %s calls %d fields %+v", r.status, g.calls, r.fields)
			}
			if tc.want == models.JobStatusSucceeded {
				key := r.fields["output_media_key"].(string)
				if key != storage.KeyForCharacter(r.job.UserID.String(), r.job.ID.String(), "png") {
					t.Fatal("character output lost image bucket namespace", key)
				}
				o, ok := s.Object(key)
				if !ok || !bytes.Equal(o.Data, tc.data) || o.ContentType != "image/png" {
					t.Fatal("missing valid durable image")
				}
			}
		})
	}
}

func TestWorkerUsesCompiledCharacterDesignAndLegacyFallback(t *testing.T) {
	t.Parallel()
	brief := "นักสำรวจผมเงิน wearing a red scarf"
	compiled, err := characterdesign.Compile(brief, characterdesign.Options{VisualStyle: "anime", Mood: "confident", DesignFormat: "full_body"}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, compiledPtr := range []*string{&compiled, nil} {
		r := &workerRepo{job: &models.VideoGenerationJob{Base: models.Base{ID: uuid.New()}, UserID: uuid.New(), Prompt: brief, CompiledPrompt: compiledPtr, AspectRatio: "3:4", EffectiveInputs: []byte(`{"model":"snapshot-model","size":"1728x2304"}`)}}
		g := &imageGenerator{data: fixturePNG()}
		if err := NewImageWorker(r, nil, g, storage.NewMemoryClient()).RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		want := brief
		if compiledPtr != nil {
			want = compiled
		}
		if g.request.Prompt != want || !strings.Contains(g.request.Prompt, brief) || g.request.Size != "1728x2304" {
			t.Fatalf("provider request lost frozen design: %+v", g.request)
		}
		if compiledPtr != nil && (!strings.Contains(g.request.Prompt, "Anime") || !strings.Contains(g.request.Prompt, "Confident") || !strings.Contains(g.request.Prompt, "A-pose")) {
			t.Fatal("provider did not receive selected design settings")
		}
	}
}

type referenceUploads struct {
	repo.UploadRepository
	upload           *models.Upload
	err              error
	seenID, seenUser string
}

func (u *referenceUploads) FindByID(_ context.Context, id, user string) (*models.Upload, error) {
	u.seenID, u.seenUser = id, user
	return u.upload, u.err
}

type referenceStorage struct {
	*storage.MemoryClient
	signErr   error
	signedKey string
	ttl       time.Duration
}

func (s *referenceStorage) SignedURL(_ context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	s.signedKey, s.ttl = key, ttl
	return "https://storage.test/fresh-reference", time.Now().Add(ttl), s.signErr
}
func TestWorkerResolvesOwnedReferenceBeforeCallingProvider(t *testing.T) {
	for _, tc := range []struct {
		name               string
		lookupErr, signErr error
		expired            bool
		want               models.JobStatus
		code               string
	}{
		{name: "owned valid reference", want: models.JobStatusSucceeded},
		{name: "other owner or missing", lookupErr: repo.ErrNotFound, want: models.JobStatusFailed, code: "IMAGE_EXPIRED"},
		{name: "expired", expired: true, want: models.JobStatusFailed, code: "IMAGE_EXPIRED"},
		{name: "signing unavailable", signErr: errors.New("private storage error"), want: models.JobStatusFailed, code: "STORAGE_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uploadID, userID := uuid.New(), uuid.New()
			expiry := time.Now().Add(time.Hour)
			if tc.expired {
				expiry = time.Now().Add(-time.Second)
			}
			uploads := &referenceUploads{upload: &models.Upload{Base: models.Base{ID: uploadID}, UserID: userID, StorageKey: "uploads/owned-reference.png", MimeType: "image/png", ExpiresAt: expiry}, err: tc.lookupErr}
			compiled, err := characterdesign.Compile("original identity", characterdesign.Options{}, true)
			if err != nil {
				t.Fatal(err)
			}
			r := &workerRepo{job: &models.VideoGenerationJob{Base: models.Base{ID: uuid.New()}, UserID: userID, UploadID: &uploadID, Prompt: "original identity", CompiledPrompt: &compiled, AspectRatio: "1:1", EffectiveInputs: []byte(`{"model":"snapshot-model"}`)}}
			g := &imageGenerator{data: fixturePNG()}
			s := &referenceStorage{MemoryClient: storage.NewMemoryClient(), signErr: tc.signErr}
			if err := NewImageWorker(r, uploads, g, s).RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			if uploads.seenID != uploadID.String() || uploads.seenUser != userID.String() {
				t.Fatal("reference lookup did not enforce job owner")
			}
			if r.status != tc.want {
				t.Fatalf("status %s fields %+v", r.status, r.fields)
			}
			if tc.want == models.JobStatusSucceeded {
				if g.calls != 1 || g.request.ReferenceURL != "https://storage.test/fresh-reference" || g.request.Prompt != compiled || !strings.Contains(g.request.Prompt, "Preserve the identity") {
					t.Fatalf("reference request lost: %+v", g.request)
				}
				if s.signedKey != uploads.upload.StorageKey || s.ttl != 10*time.Minute {
					t.Fatal("reference did not get fresh short-lived signed URL")
				}
			} else if g.calls != 0 || r.fields["failure_code"] != tc.code {
				t.Fatalf("unsafe provider call %d fields %+v", g.calls, r.fields)
			}
		})
	}
}

func TestSyntheticCharacterImageToImageSkipsAnalyzer(t *testing.T) {
	t.Parallel()
	user, uploadID := uuid.New(), uuid.New()
	prompt, err := characterdesign.CompileSynthetic("archaeologist outfit", characterdesign.Options{}, true)
	if err != nil {
		t.Fatal(err)
	}
	r := &workerRepo{job: &models.VideoGenerationJob{Base: models.Base{ID: uuid.New()}, UserID: user, UploadID: &uploadID, Prompt: "archaeologist outfit", CompiledPrompt: &prompt, AspectRatio: "1:1", EffectiveInputs: []byte(`{"model":"snapshot-model","category":"character","compilerVersion":"character-design-v2","imageGenerationMode":"image_to_image"}`)}}
	uploads := &referenceUploads{upload: &models.Upload{Base: models.Base{ID: uploadID}, UserID: user, StorageKey: "uploads/character.png", MimeType: "image/png", ExpiresAt: time.Now().Add(time.Hour)}}
	g := &imageGenerator{data: fixturePNG()}
	a := &referenceAnalyzerFake{err: errors.New("analyzer must not run")}
	s := &referenceStorage{MemoryClient: storage.NewMemoryClient()}
	if err := NewImageWorker(r, uploads, g, s).WithReferenceAnalyzer(a).RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r.status != models.JobStatusSucceeded || a.calls != 0 || g.calls != 1 || g.request.ReferenceURL != "https://storage.test/fresh-reference" || g.request.Prompt != prompt || r.preparedPrompt != "" {
		t.Fatalf("direct conditioning lost: status=%s analyzer=%d generator=%d request=%+v", r.status, a.calls, g.calls, g.request)
	}
	if uploads.seenUser != user.String() || uploads.seenID != uploadID.String() || s.signedKey != uploads.upload.StorageKey {
		t.Fatal("reference ownership or storage key lost")
	}
}

func TestImageLogValuesRejectControlsAndURLs(t *testing.T) {
	for _, value := range []string{"model\nforged=true", "https://storage.test/?token=SECRET", strings.Repeat("m", 129)} {
		if imageLogValue(value) != "unknown" {
			t.Fatal("unsafe snapshot log field")
		}
	}
	if imageLogValue("seedream-5-0-260128") != "seedream-5-0-260128" || imageLogValue("1728x2304") != "1728x2304" {
		t.Fatal("valid fields lost")
	}
}

type referenceAnalyzerFake struct {
	calls      int
	model, url string
	err        error
	cancel     context.CancelFunc
}

func (a *referenceAnalyzerFake) AnalyzeReference(_ context.Context, model, url string) (string, error) {
	a.calls++
	a.model = model
	a.url = url
	if a.cancel != nil {
		a.cancel()
	}
	return "Adult fictional character with short silver hair, red scarf and round glasses", a.err
}
func TestCharacterReferenceUsesDescriptionWithoutGenerationImage(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		analysisErr, prepareErr                error
		unconfigured, expired, foreign, cancel bool
		wantCalls                              int
	}{
		{name: "success", wantCalls: 1}, {name: "v2 success", wantCalls: 1}, {name: "analysis failure", analysisErr: errors.New("provider secret must stay private")},
		{name: "persist failure", prepareErr: errors.New("db unavailable")}, {name: "unconfigured", unconfigured: true},
		{name: "expired", expired: true}, {name: "foreign", foreign: true}, {name: "canceled", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, user := uuid.New(), uuid.New()
			r := &workerRepo{prepareErr: tc.prepareErr, job: &models.VideoGenerationJob{Base: models.Base{ID: uuid.New()}, UserID: user, UploadID: &id, Prompt: "friendly explorer", AspectRatio: "1:1", EffectiveInputs: []byte(`{"model":"snapshot-model","category":"character","imageGenerationMode":"text_to_image","referenceStrategy":"descriptive_text_v1","referenceAnalysisModel":"frozen-vision-model","referenceAnalysisVersion":"general-character-traits-v1","characterDesign":{"visualStyle":"anime","mood":"warm","designFormat":"full_body"}}`)}}
			if tc.name == "v2 success" {
				r.job.EffectiveInputs = []byte(strings.ReplaceAll(string(r.job.EffectiveInputs), "general-character-traits-v1", "visible-character-traits-v2"))
			}
			expiry := time.Now().Add(time.Hour)
			if tc.expired {
				expiry = time.Now().Add(-time.Hour)
			}
			uploads := &referenceUploads{upload: &models.Upload{Base: models.Base{ID: id}, UserID: user, MimeType: "image/png", ExpiresAt: expiry, StorageKey: "uploads/ref.png"}}
			if tc.foreign {
				uploads.err = repo.ErrNotFound
			}
			g := &imageGenerator{data: fixturePNG()}
			a := &referenceAnalyzerFake{err: tc.analysisErr}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				a.cancel = cancel
			}
			w := NewImageWorker(r, uploads, g, &referenceStorage{MemoryClient: storage.NewMemoryClient()})
			if !tc.unconfigured {
				w.WithReferenceAnalyzer(a)
			}
			if err := w.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			if g.calls != tc.wantCalls {
				t.Fatalf("generation calls=%d want=%d", g.calls, tc.wantCalls)
			}
			if tc.wantCalls == 1 {
				if a.calls != 1 || a.model != "frozen-vision-model" || a.url == "" || g.request.ReferenceURL != "" {
					t.Fatalf("wrong flow analyzer=%+v request=%+v", a, g.request)
				}
				for _, text := range []string{"friendly explorer", "silver hair", "red scarf", "Anime", "Warm", "A-pose"} {
					if !strings.Contains(g.request.Prompt, text) {
						t.Fatalf("lost %q", text)
					}
				}
				if tc.name == "v2 success" && !strings.Contains(g.request.Prompt, "only to its corresponding traits") {
					t.Fatal("v2 retained legacy global priority")
				}
				if tc.name == "success" && !strings.Contains(g.request.Prompt, "new fictional character") {
					t.Fatal("v1 replay changed")
				}
				if r.preparedPrompt != g.request.Prompt || !strings.Contains(string(r.preparedSnapshot), "referenceDescription") || strings.Contains(string(r.preparedSnapshot), a.url) {
					t.Fatal("preparation/provenance not persisted safely")
				}
			} else if r.status != models.JobStatusFailed {
				t.Fatal("failure not terminal")
			}
			if tc.prepareErr != nil && r.fields["failure_code"] != "REFERENCE_PREPARATION_FAILED" {
				t.Fatal("preparation persistence failure misclassified as analysis")
			}
			if tc.analysisErr != nil && (r.fields["failure_code"] != "REFERENCE_ANALYSIS_FAILED" || strings.Contains(r.fields["failure_message"].(string), "secret")) {
				t.Fatal("analysis diagnostic lost or unsafe")
			}
		})
	}
}

func TestPreparedReferenceReplaysFrozenPromptWithoutImageOrAnalysis(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"general-character-traits-v1", "visible-character-traits-v2", "unknown-version"} {
		t.Run(version, func(t *testing.T) {
			id := uuid.New()
			frozen := "Frozen exact prepared prompt."
			r := &workerRepo{job: &models.VideoGenerationJob{Base: models.Base{ID: uuid.New()}, UserID: uuid.New(), UploadID: &id, Prompt: "brief", CompiledPrompt: &frozen, AspectRatio: "1:1", EffectiveInputs: []byte(`{"model":"snapshot-model","category":"character","imageGenerationMode":"text_to_image","referenceStrategy":"descriptive_text_v1","referenceAnalysisVersion":"` + version + `","referenceDescription":"Face: oval; Hair: silver."}`)}}
			a := &referenceAnalyzerFake{}
			g := &imageGenerator{data: fixturePNG()}
			w := NewImageWorker(r, nil, g, storage.NewMemoryClient()).WithReferenceAnalyzer(a)
			if err := w.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			if version == "unknown-version" {
				if g.calls != 0 || r.status != models.JobStatusFailed {
					t.Fatal("unknown version replayed")
				}
				return
			}
			if a.calls != 0 || g.calls != 1 || g.request.Prompt != frozen || g.request.ReferenceURL != "" || r.preparedPrompt != "" {
				t.Fatal("frozen prepared input changed")
			}
		})
	}
}

func (r *workerRepo) EnqueueImage(_ context.Context, job *models.VideoGenerationJob, _ int64) (*models.VideoGenerationJob, error) {
	job.ID = uuid.New()
	r.job = job
	return job, nil
}

func TestSyntheticCharacterFacadeThroughImageSDKFixture(t *testing.T) {
	t.Parallel()
	user, uploadID := uuid.New(), uuid.New()
	uploads := &referenceUploads{upload: &models.Upload{Base: models.Base{ID: uploadID}, UserID: user, StorageKey: "uploads/fixture.png", MimeType: "image/png", ExpiresAt: time.Now().Add(time.Hour)}}
	r := &workerRepo{}
	s := &referenceStorage{MemoryClient: storage.NewMemoryClient()}
	f := facade.NewStudioFacade(r, nil, uploads, s, "fixture", time.Hour, "snapshot-model", 3)
	ref := uploadID.String()
	if _, err := f.CreateImage(t.Context(), user.String(), req.CreateImageRequest{Prompt: "archaeologist outfit", AspectRatio: "1:1", ReferenceImageID: &ref, IdempotencyKey: uuid.NewString()}); err != nil {
		t.Fatal(err)
	}
	before := string(r.job.EffectiveInputs)
	type imagePayload struct {
		Model  string `json:"model"`
		Image  string `json:"image"`
		Prompt string `json:"prompt"`
		Size   string `json:"size"`
	}
	payloads := make(chan imagePayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var payload imagePayload
		if request.Method != http.MethodPost || request.URL.Path != "/api/v3/images/generations" {
			t.Errorf("unexpected SDK route: %s %s", request.Method, request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		payloads <- payload
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "snapshot-model", "created": 123, "data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(fixturePNG())}}})
	}))
	defer server.Close()
	a := &referenceAnalyzerFake{err: errors.New("analyzer must not run")}
	generator := modelark.NewImageClient("fixture-key", server.URL, "wrong-default-model")
	if err := NewImageWorker(r, uploads, generator, s).WithReferenceAnalyzer(a).RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r.status != models.JobStatusSucceeded {
		t.Fatalf("fixture generation failed: %s %+v", r.status, r.fields)
	}
	select {
	case payload := <-payloads:
		if payload.Model != "snapshot-model" || payload.Image != "https://storage.test/fresh-reference" || payload.Prompt != *r.job.CompiledPrompt || payload.Size != "2048x2048" || !strings.Contains(payload.Prompt, "synthetic, fictional") {
			t.Fatalf("SDK payload lost conditioning: %+v", payload)
		}
	default:
		t.Fatal("SDK fixture received no image generation request")
	}
	if a.calls != 0 || string(r.job.EffectiveInputs) != before || r.preparedPrompt != "" {
		t.Fatal("new job analyzed or snapshot mutated")
	}
	key, ok := r.fields["output_media_key"].(string)
	if !ok {
		t.Fatal("missing durable output")
	}
	if object, ok := s.Object(key); !ok || !bytes.Equal(object.Data, fixturePNG()) {
		t.Fatal("fixture output not durably stored")
	}
}
