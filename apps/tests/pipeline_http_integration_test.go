package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/ez-gen/ez-gen-api/apps/crons"
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
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type httpPipelineStore struct {
	mu     sync.Mutex
	p      *models.StudioPipeline
	assets map[string]*models.LibraryAsset
}

func copyHTTPPipeline(p *models.StudioPipeline) *models.StudioPipeline {
	b, _ := json.Marshal(p)
	var out models.StudioPipeline
	_ = json.Unmarshal(b, &out)
	return &out
}
func (s *httpPipelineStore) Create(_ context.Context, p *models.StudioPipeline) (*models.StudioPipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p != nil {
		return copyHTTPPipeline(s.p), nil
	}
	p.ID = uuid.New()
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt
	s.p = copyHTTPPipeline(p)
	return copyHTTPPipeline(p), nil
}
func (s *httpPipelineStore) ByKey(_ context.Context, key, user string) (*models.StudioPipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p != nil && s.p.UserID.String() == user && s.p.IdempotencyKey == key {
		return copyHTTPPipeline(s.p), nil
	}
	return nil, repo.ErrNotFound
}
func (s *httpPipelineStore) Get(_ context.Context, id, user string) (*models.StudioPipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p != nil && s.p.UserID.String() == user && s.p.ID.String() == id {
		return copyHTTPPipeline(s.p), nil
	}
	return nil, repo.ErrNotFound
}
func (s *httpPipelineStore) List(_ context.Context, user string, page, limit int) ([]models.StudioPipeline, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p != nil && s.p.UserID.String() == user {
		return []models.StudioPipeline{*copyHTTPPipeline(s.p)}, 1, nil
	}
	return []models.StudioPipeline{}, 0, nil
}
func (s *httpPipelineStore) Claim(_ context.Context) (*models.StudioPipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p == nil || (s.p.Status != "queued" && s.p.Status != "processing") || s.p.LeaseToken != nil {
		return nil, repo.ErrNotFound
	}
	id := uuid.New()
	s.p.LeaseToken = &id
	s.p.Status = "processing"
	return copyHTTPPipeline(s.p), nil
}
func (s *httpPipelineStore) Advance(_ context.Context, p *models.StudioPipeline) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p.LeaseToken == nil || p.LeaseToken == nil || *s.p.LeaseToken != *p.LeaseToken {
		return repo.ErrConflict
	}
	s.p = copyHTTPPipeline(p)
	s.p.LeaseToken = nil
	return nil
}
func (s *httpPipelineStore) Retry(_ context.Context, id, user string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.p == nil || s.p.ID.String() != id || s.p.UserID.String() != user {
		return repo.ErrNotFound
	}
	if s.p.Status == "queued" || s.p.Status == "processing" {
		return nil
	}
	return repo.ErrConflict
}
func (s *httpPipelineStore) InsertAsset(_ context.Context, a *models.LibraryAsset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assets[a.ID.String()] = a
	return nil
}

type httpStudioRepo struct {
	repo.StudioRepository
	s *httpPipelineStore
	j *httpJobRepo
}

func (r *httpStudioRepo) Asset(_ context.Context, id, user string) (*models.LibraryAsset, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	a := r.s.assets[id]
	if a == nil || a.UserID.String() != user {
		return nil, repo.ErrNotFound
	}
	return a, nil
}
func (r *httpStudioRepo) EnqueueImage(c context.Context, j *models.VideoGenerationJob, _ int64) (*models.VideoGenerationJob, error) {
	if old, e := r.j.FindByIdempotencyKey(c, *j.IdempotencyKey, j.UserID.String()); e == nil {
		return old, nil
	}
	if e := r.j.Create(c, j); e != nil {
		return nil, e
	}
	return j, nil
}
func (r *httpStudioRepo) ClaimImage(_ context.Context) (*models.VideoGenerationJob, error) {
	r.j.mu.Lock()
	defer r.j.mu.Unlock()
	for _, j := range r.j.jobs {
		if j.Mode == "text_image" && j.Status == models.JobStatusQueued {
			j.Status = models.JobStatusProcessing
			copy := *j
			return &copy, nil
		}
	}
	return nil, repo.ErrNotFound
}
func (r *httpStudioRepo) FinishImage(c context.Context, id string, status models.JobStatus, fields map[string]any) error {
	return r.j.UpdateStatus(c, id, status, fields)
}

type httpJobRepo struct{ *fakeJobRepo }

func (r *httpJobRepo) Create(_ context.Context, j *models.VideoGenerationJob) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j.ID = uuid.New()
	j.CreatedAt = time.Now()
	r.jobs[j.ID.String()] = j
	return nil
}
func (r *httpJobRepo) FindByIdempotencyKey(_ context.Context, key, user string) (*models.VideoGenerationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, j := range r.jobs {
		if j.UserID.String() == user && j.IdempotencyKey != nil && *j.IdempotencyKey == key {
			copy := *j
			return &copy, nil
		}
	}
	return nil, repo.ErrNotFound
}
func (r *httpJobRepo) CountActiveByUser(context.Context, string) (int64, error) { return 0, nil }
func (r *httpJobRepo) UpdateStatus(_ context.Context, id string, status models.JobStatus, fields map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j := r.jobs[id]
	j.Status = status
	for k, v := range fields {
		switch k {
		case "provider_task_id":
			s := v.(string)
			j.ProviderTaskID = &s
		case "output_media_key":
			s := v.(string)
			j.OutputMediaKey = &s
		case "output_media_type":
			s := v.(string)
			j.OutputMediaType = &s
		case "failure_code":
			s := v.(string)
			j.FailureCode = &s
		case "failure_message":
			s := v.(string)
			j.FailureMessage = &s
		}
	}
	return nil
}

type httpUploadRepo struct {
	repo.UploadRepository
	upload *models.Upload
}

func (r *httpUploadRepo) FindByID(_ context.Context, id, user string) (*models.Upload, error) {
	if r.upload.ID.String() != id || r.upload.UserID.String() != user {
		return nil, repo.ErrNotFound
	}
	return r.upload, nil
}
func pipelineHTTP(t *testing.T, a *fiber.App, method, path string, body []byte) (int, []byte) {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, e := a.Test(request, 10000)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	data, e := io.ReadAll(response.Body)
	if e != nil {
		t.Fatal(e)
	}
	return response.StatusCode, data
}
func TestPipelineHTTPActualWorkersSDKIntegration(t *testing.T) {
	for _, source := range []string{"upload", "text", "library"} {
		t.Run(source, func(t *testing.T) { runPipelineHTTPActualWorkersSDKIntegration(t, source) })
	}
}
func runPipelineHTTPActualWorkersSDKIntegration(t *testing.T, source string) {
	t.Parallel()
	if _, e := exec.LookPath("ffprobe"); e != nil {
		t.Skip("ffprobe required for actual video persistence")
	}
	c := context.Background()
	user := uuid.New()
	sourceID := uuid.New()
	sourceKey := "uploads/" + sourceID.String() + "/source.png"
	var generated bytes.Buffer
	if e := png.Encode(&generated, image.NewRGBA(image.Rect(0, 0, 2, 2))); e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	imageCalls, videoCalls, polls := 0, 0, 0
	imageReference := ""
	var videoWire struct {
		GenerateAudio bool `json:"generate_audio"`
		Content       []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Role     string `json:"role"`
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url"`
		} `json:"content"`
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/images/generations"):
			var in struct {
				Image string `json:"image"`
			}
			if e := json.NewDecoder(r.Body).Decode(&in); e != nil {
				t.Error(e)
			}
			imageCalls++
			imageReference = in.Image
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "fixture-image", "created": 123, "data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString(generated.Bytes())}}})
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/contents/generations/tasks"):
			videoCalls++
			if e := json.NewDecoder(r.Body).Decode(&videoWire); e != nil {
				t.Error(e)
			}
			_, _ = w.Write([]byte(`{"id":"fixture-native-task"}`))
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/contents/generations/tasks/fixture-native-task"):
			polls++
			_, _ = w.Write([]byte(`{"id":"fixture-native-task","model":"fixture-video","status":"succeeded","content":{"video_url":"https://fixture.invalid/generated.mp4"}}`))
		default:
			t.Errorf("unexpected SDK route %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer provider.Close()
	store := &httpPipelineStore{assets: map[string]*models.LibraryAsset{}}
	jobs := &httpJobRepo{newFakeJobRepo()}
	studio := &httpStudioRepo{s: store, j: jobs}
	uploads := &httpUploadRepo{upload: &models.Upload{Base: models.Base{ID: sourceID}, UserID: user, StorageKey: sourceKey, MimeType: "image/png", ExpiresAt: time.Now().Add(time.Hour)}}
	media := storage.NewMemoryClient()
	reg, _ := modelark.NewRegistry("")
	imageFacade := f.NewStudioFacade(studio, jobs, uploads, media, "fixture-only", time.Hour, "fixture-image", 3)
	videoFacade := f.NewJobFacade(jobs, uploads, nil, nil, nil, nil, modelark.NewClient("fixture-only", provider.URL+"/api/v3"), reg, media, &fakeFetcher{}, 3)
	f.ConfigureJobPipeline(videoFacade, studio)
	pipeline := f.NewPipelineFacade(store, imageFacade, videoFacade, jobs, studio, uploads, reg, media, true)
	imageWorker := crons.NewImageWorker(studio, uploads, modelark.NewImageClient("fixture-only", provider.URL, "fixture-image"), media)
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		u := user.String()
		if c.Get("X-Fixture-Foreign") == "1" {
			u = uuid.NewString()
		}
		c.Locals("userID", u)
		c.Locals("requestID", uuid.NewString())
		return c.Next()
	})
	h.NewPipelineHandler(pipeline, 100).RegisterRoutes(app)
	speech := "สวัสดี \"ทุกคน\"\nยินดีที่ได้พบ"
	input := req.CreatePipelineRequest{ReferenceImageID: sourceID.String(), CharacterPrompt: "ตัวละครสังเคราะห์คงใบหน้าเดิม", ScenePrompt: "ตัวละครเดินในสวน แสงเย็น", Dialogue: speech, IdempotencyKey: uuid.NewString()}
	expectedImageCalls := 1
	if source == "text" {
		input.ReferenceImageID = ""
	}
	if source == "library" {
		input.ReferenceImageID = ""
		input.CharacterPrompt = ""
		expectedImageCalls = 0
		assetID := uuid.New()
		jobID := uuid.New()
		key := "characters/fixture-existing.png"
		kind := "image"
		jobs.jobs[jobID.String()] = &models.VideoGenerationJob{Base: models.Base{ID: jobID}, UserID: user, Mode: "text_image", Status: models.JobStatusSucceeded, OutputMediaKey: &key, OutputMediaType: &kind, EffectiveInputs: []byte(`{"category":"character"}`)}
		store.assets[assetID.String()] = &models.LibraryAsset{Base: models.Base{ID: assetID}, UserID: user, AssetType: "character", JobID: &jobID, StorageKey: &key, MediaType: &kind}
		input.CharacterAssetID = assetID.String()
	}
	rejected := input
	rejected.ScenePrompt = strings.Repeat("ก", 1500)
	rejected.Dialogue = "สวัสดีครับทุกคน"
	rejectedBody, _ := json.Marshal(rejected)
	rejectedStatus, rejectedRaw := pipelineHTTP(t, app, "POST", "/studio-pipelines", rejectedBody)
	var rejectedOut struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Field   string `json:"field"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"details"`
	}
	if err := json.Unmarshal(rejectedRaw, &rejectedOut); err != nil {
		t.Fatal(err)
	}
	expectedMessage := "รายละเอียดฉากและบทพูดรวมกันยาวเกินขีดจำกัด กรุณาย่อฉากหรือบทพูดแล้วลองอีกครั้ง"
	if rejectedStatus != 400 || rejectedOut.Code != "VALIDATION_ERROR" || rejectedOut.Message != expectedMessage || len(rejectedOut.Details) != 1 || rejectedOut.Details[0].Field != "scenePrompt" || rejectedOut.Details[0].Reason != "prompt_budget" || rejectedOut.Details[0].Message != expectedMessage {
		t.Fatalf("budget validation %d %s", rejectedStatus, rejectedRaw)
	}
	if store.p != nil || imageCalls != 0 || videoCalls != 0 {
		t.Fatal("rejected request created pipeline or child calls")
	}
	if bytes.Contains(rejectedRaw, []byte(rejected.ScenePrompt)) || bytes.Contains(rejectedRaw, []byte(rejected.Dialogue)) || bytes.Contains(rejectedRaw, []byte("dialogue exceeds prompt budget")) {
		t.Fatal("validation leaked user input or compiler diagnostics")
	}
	body, _ := json.Marshal(input)
	status, raw := pipelineHTTP(t, app, "POST", "/studio-pipelines", body)
	if status != 201 {
		t.Fatalf("POST %d %s", status, raw)
	}
	var out struct {
		Data dto.PipelineDTO `json:"data"`
	}
	if e := json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	id := out.Data.ID
	status, raw = pipelineHTTP(t, app, "POST", "/studio-pipelines", body)
	if status != 201 {
		t.Fatalf("replay %d %s", status, raw)
	}
	var replay struct {
		Data dto.PipelineDTO `json:"data"`
	}
	_ = json.Unmarshal(raw, &replay)
	if replay.Data.ID != id {
		t.Fatal("POST replay new aggregate")
	}
	for range 12 {
		if e := pipeline.RunOnce(c); e != nil {
			t.Fatal(e)
		}
		if e := imageWorker.RunOnce(c); e != nil {
			t.Fatal(e)
		}
		if store.p.VideoJobID != nil {
			j, e := jobs.FindByID(c, store.p.VideoJobID.String(), user.String())
			if e != nil {
				t.Fatal(e)
			}
			if j.ProviderTaskID != nil && !j.Status.IsTerminal() {
				if e = videoFacade.PollAndUpdate(c, j); e != nil {
					t.Fatal(e)
				}
			}
		}
	}
	status, raw = pipelineHTTP(t, app, "GET", "/studio-pipelines/"+id, nil)
	if status != 200 {
		t.Fatalf("GET %d %s", status, raw)
	}
	_ = json.Unmarshal(raw, &out)
	if out.Data.Status != "succeeded" || out.Data.Stage != "done" || out.Data.CharacterAssetID == nil || out.Data.VideoAssetID == nil || len(store.assets) != 2 {
		t.Fatalf("full pipeline %s", raw)
	}
	mu.Lock()
	defer mu.Unlock()
	quoted, _ := json.Marshal(speech)
	if imageCalls != expectedImageCalls || (source == "upload" && !strings.Contains(imageReference, sourceKey)) || (source != "upload" && imageReference != "") || videoCalls != 1 || polls != 1 || !videoWire.GenerateAudio || len(videoWire.Content) != 2 || videoWire.Content[1].Role != "reference_image" || strings.Contains(videoWire.Content[1].ImageURL.URL, sourceKey) || !strings.Contains(videoWire.Content[0].Text, string(quoted)) || !strings.Contains(videoWire.Content[0].Text, input.ScenePrompt) {
		t.Fatalf("SDK lineage images=%d videos=%d polls=%d wire=%+v", imageCalls, videoCalls, polls, videoWire)
	}
	character := store.assets[*out.Data.CharacterAssetID]
	video := store.assets[*out.Data.VideoAssetID]
	if character.StorageKey == nil || video.StorageKey == nil || !strings.Contains(videoWire.Content[1].ImageURL.URL, *character.StorageKey) || character.AssetType != "character" || video.AssetType != "video" {
		t.Fatal("durable generated lineage invalid")
	}
	if _, e := pipeline.Get(c, id, uuid.NewString()); !errors.Is(e, f.ErrNotFound) {
		t.Fatal("owner isolation", e)
	}
	status, raw = pipelineHTTP(t, app, "GET", "/studio-pipelines?page=1&limit=20", nil)
	if status != 200 || !bytes.Contains(raw, []byte(`"total":1`)) {
		t.Fatalf("list %d %s", status, raw)
	}
	status, _ = pipelineHTTP(t, app, "POST", "/studio-pipelines/"+id+"/retry", []byte(`{}`))
	if status != 409 {
		t.Fatal("succeeded retry", status)
	}
	status, _ = pipelineHTTP(t, app, "GET", "/studio-pipelines?limit=51", nil)
	if status != 400 {
		t.Fatal("pagination validation", status)
	}
	changed := input
	changed.Dialogue += "อื่น"
	bad, _ := json.Marshal(changed)
	status, _ = pipelineHTTP(t, app, "POST", "/studio-pipelines", bad)
	if status != 409 {
		t.Fatal("changed-key conflict", status)
	}
}
