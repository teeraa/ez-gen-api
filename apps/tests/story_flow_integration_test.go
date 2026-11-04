package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	facade "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	handler "github.com/ez-gen/ez-gen-api/apps/handlers/handler_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type sfMemory struct {
	mu   sync.Mutex
	rows map[string]models.StoryFlowProject
}

func (sf *sfMemory) Create(_ context.Context, p *models.StoryFlowProject) (*models.StoryFlowProject, error) {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	for _, v := range sf.rows {
		if v.UserID == p.UserID && v.IdempotencyKey == p.IdempotencyKey {
			copy := v
			return &copy, nil
		}
	}
	p.ID = uuid.New()
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt
	sf.rows[p.ID.String()] = *p
	return p, nil
}
func (sf *sfMemory) Get(_ context.Context, id, user string) (*models.StoryFlowProject, error) {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	v, ok := sf.rows[id]
	if !ok || v.UserID.String() != user {
		return nil, repo.ErrNotFound
	}
	return &v, nil
}
func (sf *sfMemory) ByKey(_ context.Context, key, user string) (*models.StoryFlowProject, error) {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	for _, v := range sf.rows {
		if v.IdempotencyKey == key && v.UserID.String() == user {
			copy := v
			return &copy, nil
		}
	}
	return nil, repo.ErrNotFound
}
func (sf *sfMemory) List(_ context.Context, user string, page, limit int) ([]models.StoryFlowProject, int64, error) {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	rows := []models.StoryFlowProject{}
	for _, v := range sf.rows {
		if v.UserID.String() == user {
			rows = append(rows, v)
		}
	}
	return rows, int64(len(rows)), nil
}
func (sf *sfMemory) Change(_ context.Context, id, user string, fn func(*models.StoryFlowProject) error) error {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	p, ok := sf.rows[id]
	if !ok || p.UserID.String() != user {
		return repo.ErrNotFound
	}
	if e := fn(&p); e != nil {
		return e
	}
	sf.rows[id] = p
	return nil
}
func (sf *sfMemory) Claim(_ context.Context) (*models.StoryFlowProject, error) {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	for id, v := range sf.rows {
		if (v.Status == "queued" || v.Status == "processing") && v.LeaseToken == nil {
			token := uuid.New()
			v.LeaseToken = &token
			sf.rows[id] = v
			return &v, nil
		}
	}
	return nil, repo.ErrNotFound
}
func (sf *sfMemory) Save(_ context.Context, p *models.StoryFlowProject, release bool) error {
	sf.mu.Lock()
	defer sf.mu.Unlock()
	old := sf.rows[p.ID.String()]
	if old.LeaseToken == nil || p.LeaseToken == nil || *old.LeaseToken != *p.LeaseToken {
		return repo.ErrConflict
	}
	v := *p
	if release {
		v.LeaseToken = nil
		v.LeaseUntil = nil
	}
	sf.rows[p.ID.String()] = v
	return nil
}

type sfAssets struct {
	entries map[string]models.LibraryAsset
}

func (a sfAssets) Asset(_ context.Context, id, user string) (*models.LibraryAsset, error) {
	v, ok := a.entries[id]
	if !ok || v.UserID.String() != user {
		return nil, repo.ErrNotFound
	}
	return &v, nil
}

type sfMedia struct {
	imageOverride []byte
	mu            sync.Mutex
	objects       map[string][]byte
	images        int
	imageRequests []modelark.ImageRequest
}

func (s *sfMedia) Upload(_ context.Context, key string, data []byte, kind string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = bytes.Clone(data)
	return nil
}
func (s *sfMedia) SignedURL(_ context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	return "https://owned.example/" + key, time.Now().Add(ttl), nil
}
func (s *sfMedia) SignedDownloadURL(c context.Context, key string, ttl time.Duration, name string) (string, time.Time, error) {
	return s.SignedURL(c, key, ttl)
}
func (s *sfMedia) GenerateImage(_ context.Context, in modelark.ImageRequest) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.images++
	s.imageRequests = append(s.imageRequests, in)
	if s.imageOverride != nil {
		return bytes.Clone(s.imageOverride), nil
	}
	var buf bytes.Buffer
	if e := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 9))); e != nil {
		return nil, e
	}
	return buf.Bytes(), nil
}
func (s *sfMedia) Fetch(_ context.Context, u string) ([]byte, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.HasPrefix(u, "https://provider.example/") {
		return []byte(strings.TrimPrefix(u, "https://provider.example/")), "video/mp4", nil
	}
	key := strings.TrimPrefix(u, "https://owned.example/")
	b, ok := s.objects[key]
	if !ok {
		return nil, "", errors.New("missing fixture")
	}
	return bytes.Clone(b), "video/mp4", nil
}

type sfVideo struct {
	requests    []modelark.CreateTaskRequest
	failTask    string
	failOnce    bool
	unknown     bool
	pollFailure bool
}

func (v *sfVideo) CreateTask(_ context.Context, in modelark.CreateTaskRequest) (string, error) {
	v.requests = append(v.requests, in)
	if v.unknown {
		return "", errors.New("unknown provider response")
	}
	return fmt.Sprintf("task%d", len(v.requests)), nil
}
func (v *sfVideo) GetTask(_ context.Context, id string) (*modelark.TaskResult, error) {
	if v.pollFailure {
		v.pollFailure = false
		return nil, errors.New("temporary poll error")
	}
	if id == v.failTask && !v.failOnce {
		v.failOnce = true
		return &modelark.TaskResult{Status: modelark.ProviderStatusFailed}, nil
	}
	return &modelark.TaskResult{Status: modelark.ProviderStatusSucceeded, VideoURL: "https://provider.example/" + id}, nil
}
func (*sfVideo) CancelTask(context.Context, string) error { return nil }

type sfMerge struct {
	ordered []string
	calls   int
}

func (m *sfMerge) Merge(_ context.Context, in videoprocessing.MergeRequest) ([]byte, error) {
	m.calls++
	for _, c := range in.Clips {
		b, e := os.ReadFile(c.Path)
		if e != nil {
			return nil, e
		}
		m.ordered = append(m.ordered, string(b))
	}
	if in.OutputAspectRatio != "16:9" || in.OutputResolution != "720p" {
		return nil, errors.New("wrong normalized output settings")
	}
	return []byte("fixture merged mp4"), nil
}

type sfAnalyzer struct {
	failure      error
	prompts      []string
	models       []string
	characterIDs []string
	calls        int
	invalid      bool
	sceneCount   int
	shots        []int
}

func (a *sfAnalyzer) Analyze(_ context.Context, model, prompt string) (string, error) {
	a.calls++
	a.prompts = append(a.prompts, prompt)
	a.models = append(a.models, model)
	if a.failure != nil {
		return "", a.failure
	}
	if model != "analysis-fixture" {
		return "", errors.New("wrong pinned model")
	}
	continuity := models.StoryFlowContinuity{Wardrobe: "blue coat", Lighting: "morning", CharacterState: "walking"}
	if !strings.Contains(prompt, "sceneShots") {
		scenes := []models.StoryFlowScene{}
		for i := 0; i < a.sceneCount; i++ {
			scenes = append(scenes, models.StoryFlowScene{SceneIndex: i + 1, Title: "ฉาก", Narrative: "เรื่องราว", Location: "สวน", Characters: append([]string{}, a.characterIDs...), Emotion: "อบอุ่น", Continuity: continuity, Shots: []models.StoryFlowShot{}})
		}
		if a.invalid {
			scenes = nil
		}
		b, _ := json.Marshal(struct {
			Scenes []models.StoryFlowScene `json:"scenes"`
		}{scenes})
		return string(b), nil
	}
	scenes := []struct {
		SceneIndex int                    `json:"sceneIndex"`
		Shots      []models.StoryFlowShot `json:"shots"`
	}{}
	for i, n := range a.shots {
		shots := []models.StoryFlowShot{}
		for j := 0; j < n; j++ {
			shots = append(shots, models.StoryFlowShot{SceneIndex: i + 1, ShotIndex: j + 1, Action: "เดิน", Characters: append([]string{}, a.characterIDs...), Location: "สวน", Atmosphere: "อบอุ่น", Framing: "wide", CameraAngle: "eye level", CameraMovement: "slow pan", Continuity: continuity, ImagePrompt: "garden scene", VideoPrompt: "walk in garden", DurationSeconds: 4})
		}
		scenes = append(scenes, struct {
			SceneIndex int                    `json:"sceneIndex"`
			Shots      []models.StoryFlowShot `json:"shots"`
		}{i + 1, shots})
	}
	b, _ := json.Marshal(struct {
		Scenes any `json:"scenes"`
	}{scenes})
	return string(b), nil
}

type sfFixture struct {
	assets   *sfAssets
	f        *facade.StoryFlowFacade
	media    *sfMedia
	video    *sfVideo
	analysis *sfAnalyzer
	merge    *sfMerge
	app      *fiber.App
	user     string
	input    req.CreateStoryFlowRequest
}

func sfNew(t *testing.T, store interface {
	Create(context.Context, *models.StoryFlowProject) (*models.StoryFlowProject, error)
	Get(context.Context, string, string) (*models.StoryFlowProject, error)
	ByKey(context.Context, string, string) (*models.StoryFlowProject, error)
	List(context.Context, string, int, int) ([]models.StoryFlowProject, int64, error)
	Change(context.Context, string, string, func(*models.StoryFlowProject) error) error
	Claim(context.Context) (*models.StoryFlowProject, error)
	Save(context.Context, *models.StoryFlowProject, bool) error
}) *sfFixture {
	t.Helper()
	registry, e := modelark.NewRegistry("dreamina-seedance-2-0-fast-260128")
	if e != nil {
		t.Fatal(e)
	}
	media := &sfMedia{objects: map[string][]byte{}}
	video := &sfVideo{}
	analysis := &sfAnalyzer{sceneCount: 2, shots: []int{2, 1}}
	merge := &sfMerge{}
	assets := &sfAssets{entries: map[string]models.LibraryAsset{}}
	f := facade.NewStoryFlowFacade(store, assets, registry, media, analysis, video, media, media, merge, facade.StoryFlowOptions{AnalysisModel: "analysis-fixture", ImageModel: "image-fixture", Enabled: true, ExportAvailable: true, OwnedMediaFetcher: media})
	user := uuid.NewString()
	app := fiber.New()
	g := app.Group("/open/v1", func(c *fiber.Ctx) error {
		u := c.Get("X-Fixture-User")
		if u == "" {
			u = user
		}
		c.Locals("userID", u)
		c.Locals("requestID", "fixture-request")
		return c.Next()
	})
	handler.NewStoryFlowHandler(f, 1000).RegisterRoutes(g)
	return &sfFixture{assets, f, media, video, analysis, merge, app, user, req.CreateStoryFlowRequest{Title: "fixture story", Idea: "a garden journey", Style: "anime", ModelID: "violet-motion-2", Quality: "standard", AspectRatio: "16:9", SceneCount: 2, MoodTags: []string{"warm"}, CharacterAssetIDs: []string{}, IdempotencyKey: uuid.NewString()}}
}
func (sf *sfFixture) http(t *testing.T, method, path string, body any, user string) (int, []byte) {
	t.Helper()
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, "/open/v1/story-flow"+path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	if user != "" {
		r.Header.Set("X-Fixture-User", user)
	}
	res, e := sf.app.Test(r, -1)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}
func (sf *sfFixture) project(t *testing.T, method, path string, body any) *dto.StoryFlowProjectDTO {
	t.Helper()
	status, b := sf.http(t, method, path, body, "")
	if status < 200 || status > 299 {
		t.Fatalf("%s %s status=%d %s", method, path, status, b)
	}
	var env struct {
		Data dto.StoryFlowProjectDTO `json:"data"`
	}
	if e := json.Unmarshal(b, &env); e != nil {
		t.Fatal(e)
	}
	return &env.Data
}
func (sf *sfFixture) action(t *testing.T, p *dto.StoryFlowProjectDTO, name string, shots []req.StoryFlowSceneShots, shot string) *dto.StoryFlowProjectDTO {
	t.Helper()
	return sf.project(t, "POST", "/projects/"+p.ID+"/"+name, req.StoryFlowActionRequest{Revision: 1, IdempotencyKey: uuid.NewString(), SceneShots: shots, Stage: p.Stage, ShotID: shot})
}
func (sf *sfFixture) until(t *testing.T, id string, done func(*dto.StoryFlowProjectDTO) bool) *dto.StoryFlowProjectDTO {
	t.Helper()
	for i := 0; i < 40; i++ {
		p := sf.project(t, "GET", "/projects/"+id, nil)
		if done(p) {
			return p
		}
		if e := sf.f.Tick(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	t.Fatal("fixture state never reached")
	return nil
}
func (sf *sfFixture) storyboard(t *testing.T) *dto.StoryFlowProjectDTO {
	t.Helper()
	p := sf.project(t, "POST", "/projects", sf.input)
	p = sf.action(t, p, "story", nil, "")
	p = sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Stage == "story_review" })
	p = sf.action(t, p, "storyboard", []req.StoryFlowSceneShots{{SceneIndex: 1, ShotCount: 2}, {SceneIndex: 2, ShotCount: 1}}, "")
	return sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Stage == "storyboard_review" })
}

func TestStoryFlowHTTPFullPartialRetryAndOrdering(t *testing.T) {
	t.Parallel()
	sf := sfNew(t, &sfMemory{rows: map[string]models.StoryFlowProject{}})
	sfRunFull(t, sf)
}
func sfRunFull(t *testing.T, sf *sfFixture) {
	t.Helper()
	p := sf.storyboard(t)
	if p.TotalShots != 3 || p.CompletedImages != 3 || sf.media.images != 3 {
		t.Fatalf("wrong image count %+v", p)
	}
	for _, sc := range p.Scenes {
		for _, sh := range sc.Shots {
			if !strings.Contains(sh.ImagePrompt, "Anime animation") || strings.Contains(sh.VideoPrompt, "live action") {
				t.Fatal("style missing/conflicting")
			}
		}
	}
	replay := sf.project(t, "POST", "/projects", sf.input)
	if replay.ID != p.ID {
		t.Fatal("create replay duplicated")
	}
	changed := sf.input
	changed.Idea = "different"
	status, _ := sf.http(t, "POST", "/projects", changed, "")
	if status != 409 {
		t.Fatalf("key changed payload status %d", status)
	}
	status, b := sf.http(t, "GET", "/projects/"+p.ID, nil, uuid.NewString())
	if status != 404 || strings.Contains(string(b), p.ID) {
		t.Fatalf("ownership leak %d %s", status, b)
	}
	sf.video.failTask = "task2"
	p = sf.action(t, p, "video", nil, "")
	p = sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Status == "failed" })
	if p.CompletedVideos != 1 || len(sf.video.requests) != 2 {
		t.Fatal("partial failure lost successful work")
	}
	failed := p.Scenes[0].Shots[1]
	if !failed.CanRetry {
		t.Fatal("failed shot not retryable")
	}
	p = sf.action(t, p, "retry", nil, failed.ID)
	p = sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Status == "succeeded" })
	if p.CompletedVideos != 3 || p.OutputURL == nil || p.DownloadURL == nil || len(sf.video.requests) != 4 || sf.analysis.calls != 2 || sf.media.images != 3 {
		t.Fatalf("retry changed successful work %+v requests=%d", p, len(sf.video.requests))
	}
	if strings.Join(sf.merge.ordered, ",") != "task1,task3,task4" {
		t.Fatalf("export order %v", sf.merge.ordered)
	}
	if sf.video.requests[1].Content[0].Text != sf.video.requests[2].Content[0].Text || sf.video.requests[1].Content[1].URL != sf.video.requests[2].Content[1].URL {
		t.Fatal("retry mutated shot snapshot")
	}
	// GET after completion reconstructs durable media and correct revision without local browser state.
	reload := sf.project(t, "GET", "/projects/"+p.ID, nil)
	if reload.Revision != 1 || reload.OutputURL == nil || reload.TotalShots != 3 {
		t.Fatal("reload lost work")
	}
	status, _ = sf.http(t, "POST", "/projects/"+p.ID+"/video", req.StoryFlowActionRequest{Revision: 2, IdempotencyKey: uuid.NewString()}, "")
	if status != 409 {
		t.Fatal("stale revision accepted")
	}
}
func TestStoryFlowInvalidAnalysisNeverSucceeds(t *testing.T) {
	t.Parallel()
	sf := sfNew(t, &sfMemory{rows: map[string]models.StoryFlowProject{}})
	sf.analysis.invalid = true
	p := sf.project(t, "POST", "/projects", sf.input)
	p = sf.action(t, p, "story", nil, "")
	p = sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Status == "failed" })
	if p.FailureCode != "ANALYSIS_INVALID_OUTPUT" || len(p.Scenes) != 0 || sf.media.images != 0 {
		t.Fatal("invalid model output saved")
	}
	sf.analysis.invalid = false
	p = sf.action(t, p, "retry", nil, "")
	sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Stage == "story_review" })
}
func TestStoryFlowUnknownSubmissionNeverResubmits(t *testing.T) {
	t.Parallel()
	sf := sfNew(t, &sfMemory{rows: map[string]models.StoryFlowProject{}})
	p := sf.storyboard(t)
	sf.video.unknown = true
	p = sf.action(t, p, "video", nil, "")
	p = sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Status == "needs_attention" })
	if len(sf.video.requests) != 1 || p.CanRetry {
		t.Fatal("ambiguous submission retryable")
	}
	status, _ := sf.http(t, "POST", "/projects/"+p.ID+"/retry", req.StoryFlowActionRequest{Revision: 1, IdempotencyKey: uuid.NewString(), Stage: "video", ShotID: p.Scenes[0].Shots[0].ID}, "")
	if status != 409 {
		t.Fatal("ambiguous retry accepted")
	}
	_ = sf.f.Tick(context.Background())
	if len(sf.video.requests) != 1 {
		t.Fatal("automatically duplicated unknown submission")
	}
}
func TestStoryFlowActionReplayAndShotCountValidation(t *testing.T) {
	t.Parallel()
	sf := sfNew(t, &sfMemory{rows: map[string]models.StoryFlowProject{}})
	p := sf.project(t, "POST", "/projects", sf.input)
	in := req.StoryFlowActionRequest{Revision: 1, IdempotencyKey: uuid.NewString()}
	p = sf.project(t, "POST", "/projects/"+p.ID+"/story", in)
	sf.project(t, "POST", "/projects/"+p.ID+"/story", in)
	in.Revision = 2
	status, _ := sf.http(t, "POST", "/projects/"+p.ID+"/story", in, "")
	if status != 409 {
		t.Fatal("action key changed input accepted")
	}
	p = sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Stage == "story_review" })
	status, _ = sf.http(t, "POST", "/projects/"+p.ID+"/storyboard", req.StoryFlowActionRequest{Revision: 1, IdempotencyKey: uuid.NewString(), SceneShots: []req.StoryFlowSceneShots{{SceneIndex: 1, ShotCount: 20}, {SceneIndex: 2, ShotCount: 1}}}, "")
	if status != 400 {
		t.Fatal("shot total > processor limit accepted")
	}
}
func TestStoryFlowPostgresHTTPFullFlow(t *testing.T) {
	dsn := os.Getenv("STORY_FLOW_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("STORY_FLOW_TEST_DATABASE_URL unset; disposable loopback fixture only")
	}
	// Refuse every destination except the explicitly named disposable test database.
	parsed, parseErr := pgx.ParseConfig(dsn)
	if parseErr != nil || parsed.Host != "127.0.0.1" || parsed.Database != "story_flow_test" || parsed.User != "story_flow_test" || len(parsed.Fallbacks) > 0 {
		t.Fatal("refusing nonfixture database")
	}
	database, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	sqlDB, _ := database.DB()
	defer sqlDB.Close()
	// This test is opt-in and owns this exact disposable database's fixture schema.
	if e = database.Exec("DROP TABLE IF EXISTS story_flow_projects; CREATE TABLE IF NOT EXISTS users (id UUID PRIMARY KEY)").Error; e != nil {
		t.Fatal(e)
	}
	migration, e := os.ReadFile(filepath.Join("..", "..", "migrations", "020_story_flow.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if e = database.Exec(string(migration)).Error; e != nil {
		t.Fatal(e)
	}
	store := repo.NewStoryFlowRepository(database, 3)
	sf := sfNew(t, store)
	if e = database.Exec("INSERT INTO users(id) VALUES (?) ON CONFLICT DO NOTHING", sf.user).Error; e != nil {
		t.Fatal(e)
	}
	sfRunFull(t, sf)

	// Worker claims and writes are fenced across lease expiry on the actual database.
	nextInput := sf.input
	nextInput.IdempotencyKey = uuid.NewString()
	next := sf.project(t, "POST", "/projects", nextInput)
	sf.action(t, next, "story", nil, "")
	first, err := store.Claim(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Claim(context.Background()); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("duplicate claim: %v", err)
	}
	if err = database.Model(&models.StoryFlowProject{}).Where("id=?", first.ID).Update("lease_until", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	second, err := store.Claim(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Save(context.Background(), first, true); !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("stale lease accepted: %v", err)
	}
	if err = store.Save(context.Background(), second, true); err != nil {
		t.Fatal(err)
	}
	// Concurrent replay must converge via transaction/unique key; no duplicate project.
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, e := sf.f.Create(context.Background(), sf.user, sf.input)
			if e != nil {
				errs <- e
				return
			}
			ids <- p.ID
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	close(ids)
	id := ""
	for v := range ids {
		if id != "" && id != v {
			t.Fatal("concurrent idempotency duplicated")
		}
		id = v
	}
}

func TestStoryFlowInterruptedImageSubmissionNeedsAttention(t *testing.T) {
	t.Parallel()
	store := &sfMemory{rows: map[string]models.StoryFlowProject{}}
	sf := sfNew(t, store)
	p := sf.storyboard(t)
	if e := store.Change(context.Background(), p.ID, sf.user, func(row *models.StoryFlowProject) error {
		s, e := dto.DecodeStoryFlow(row)
		if e != nil {
			return e
		}
		s.Stage = "storyboard"
		s.Status = "queued"
		s.Scenes[0].Shots[0].ImageStatus = "submitting"
		row.Status = s.Status
		row.Document, e = json.Marshal(s)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if e := sf.f.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	p = sf.project(t, "GET", "/projects/"+p.ID, nil)
	if p.Status != "needs_attention" || sf.media.images != 3 {
		t.Fatal("interrupted image automatically regenerated")
	}
}
func TestStoryFlowPollingRetryPreservesTask(t *testing.T) {
	t.Parallel()
	sf := sfNew(t, &sfMemory{rows: map[string]models.StoryFlowProject{}})
	p := sf.storyboard(t)
	sf.video.pollFailure = true
	p = sf.action(t, p, "video", nil, "")
	p = sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Status == "failed" })
	if len(sf.video.requests) != 1 {
		t.Fatal("unexpected generation count")
	}
	p = sf.action(t, p, "retry", nil, p.Scenes[0].Shots[0].ID)
	sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Status == "succeeded" })
	if len(sf.video.requests) != 3 {
		t.Fatal("poll recovery duplicated generation")
	}
}
func TestStoryFlowInputValidation(t *testing.T) {
	t.Parallel()
	sf := sfNew(t, &sfMemory{rows: map[string]models.StoryFlowProject{}})
	cases := []struct {
		name   string
		change func(*req.CreateStoryFlowRequest)
	}{
		{"title", func(i *req.CreateStoryFlowRequest) { i.Title = "" }},
		{"idea", func(i *req.CreateStoryFlowRequest) { i.Idea = strings.Repeat("x", 6001) }},
		{"moodTags", func(i *req.CreateStoryFlowRequest) { i.MoodTags = []string{"1", "2", "3", "4", "5", "6"} }},
		{"sceneCount", func(i *req.CreateStoryFlowRequest) { i.SceneCount = 11 }},
		{"style", func(i *req.CreateStoryFlowRequest) { i.Style = "other" }},
		{"quality", func(i *req.CreateStoryFlowRequest) { i.Quality = "high" }},
		{"modelId", func(i *req.CreateStoryFlowRequest) { i.ModelID = "imaginary" }},
		{"characterAssetIds", func(i *req.CreateStoryFlowRequest) { i.CharacterAssetIDs = []string{"bad"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := sf.input
			in.IdempotencyKey = uuid.NewString()
			tc.change(&in)
			status, b := sf.http(t, "POST", "/projects", in, "")
			if status != 400 || !strings.Contains(string(b), tc.name) {
				t.Fatalf("%d %s", status, b)
			}
		})
	}
}
func TestStoryFlowImmutableCreateReplayWhenDisabled(t *testing.T) {
	t.Parallel()
	store := &sfMemory{rows: map[string]models.StoryFlowProject{}}
	sf := sfNew(t, store)
	p := sf.project(t, "POST", "/projects", sf.input)
	registry, _ := modelark.NewRegistry("")
	disabled := facade.NewStoryFlowFacade(store, sfAssets{}, registry, sf.media, sf.analysis, sf.video, sf.media, sf.media, sf.merge, facade.StoryFlowOptions{})
	replay, e := disabled.Create(context.Background(), sf.user, sf.input)
	if e != nil || replay.ID != p.ID {
		t.Fatalf("immutable replay depended on current configuration: %v", e)
	}
}

func TestStoryFlowInterruptedAnalysisNeverResubmits(t *testing.T) {
	t.Parallel()
	store := &sfMemory{rows: map[string]models.StoryFlowProject{}}
	sf := sfNew(t, store)
	p := sf.project(t, "POST", "/projects", sf.input)
	p = sf.action(t, p, "story", nil, "")
	if e := store.Change(context.Background(), p.ID, sf.user, func(row *models.StoryFlowProject) error {
		s, e := dto.DecodeStoryFlow(row)
		if e != nil {
			return e
		}
		s.AnalysisStatuses = map[string]string{"story": "submitting"}
		s.AnalysisPrompt = "immutable analysis fixture"
		row.Document, e = json.Marshal(s)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if e := sf.f.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	p = sf.project(t, "GET", "/projects/"+p.ID, nil)
	if p.Status != "needs_attention" || sf.analysis.calls != 0 {
		t.Fatal("interrupted analysis automatically resubmitted")
	}
}

func TestStoryFlowLongLibraryDescriptionsAndAppearingReference(t *testing.T) {
	t.Parallel()
	store := &sfMemory{rows: map[string]models.StoryFlowProject{}}
	sf := sfNew(t, store)
	first, absent := uuid.NewString(), uuid.NewString()
	uid := uuid.MustParse(sf.user)
	key1, key2 := "characters/fixture/first.png", "characters/fixture/absent.png"
	sf.assets.entries[first] = models.LibraryAsset{UserID: uid, Name: "ตัวแรก", AssetType: "character", Description: strings.Repeat("blue coat ", 190), Prompt: strings.Repeat("appearance ", 180), StorageKey: &key1}
	sf.assets.entries[absent] = models.LibraryAsset{UserID: uid, Name: "ไม่ปรากฏ", AssetType: "character", Description: strings.Repeat("red coat ", 190), Prompt: strings.Repeat("unused ", 180), StorageKey: &key2}
	sf.input.CharacterAssetIDs = []string{first, absent}
	sf.analysis.characterIDs = []string{first}
	p := sf.storyboard(t)
	for _, r := range sf.media.imageRequests {
		if r.ReferenceURL != "https://owned.example/"+key1 || strings.Contains(r.Prompt, absent) || len([]rune(r.Prompt)) > 2000 {
			t.Fatal("appearing reference/budget mismatch")
		}
	}
	row, e := store.Get(context.Background(), p.ID, sf.user)
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := dto.DecodeStoryFlow(row)
	if e != nil {
		t.Fatal(e)
	}
	if len([]rune(snapshot.Characters[0].AppearanceExcerpt)) > 160 || !strings.Contains(snapshot.AnalysisPrompts["story"], strings.Repeat("appearance ", 100)) {
		t.Fatal("full analysis snapshot or pinned excerpt lost")
	}
}
func TestStoryFlowRealisticNewCharacterBaseline(t *testing.T) {
	t.Parallel()
	store := &sfMemory{rows: map[string]models.StoryFlowProject{}}
	sf := sfNew(t, store)
	sf.input.Style = "realistic"
	sf.input.NewCharacterPrompt = "a fictional explorer wearing a blue coat"
	sf.analysis.characterIDs = []string{"new-character"}
	p := sf.storyboard(t)
	if sf.media.images != 4 || !strings.Contains(sf.media.imageRequests[0].Prompt, "Visibly stylized 3D fictional character") {
		t.Fatal("current synthetic baseline not used")
	}
	for _, sc := range p.Scenes {
		for _, sh := range sc.Shots {
			if !strings.Contains(sh.VideoPrompt, "Photorealistic cinematic live action") || !strings.Contains(sh.VideoPrompt, "Replace CG surface rendering") {
				t.Fatal("realistic video appearance boundary missing")
			}
		}
	}
}

func TestStoryFlowTruncatedBitmapNeverReady(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"png", "jpeg"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			var encoded bytes.Buffer
			bitmap := image.NewRGBA(image.Rect(0, 0, 16, 9))
			var e error
			if format == "png" {
				e = png.Encode(&encoded, bitmap)
			} else {
				e = jpeg.Encode(&encoded, bitmap, nil)
			}
			if e != nil {
				t.Fatal(e)
			}
			complete := encoded.Bytes()
			var truncated []byte
			for n := 1; n < len(complete); n++ {
				if _, actual, err := image.DecodeConfig(bytes.NewReader(complete[:n])); err == nil && actual == format {
					if _, _, err = image.Decode(bytes.NewReader(complete[:n])); err != nil {
						truncated = bytes.Clone(complete[:n])
						break
					}
				}
			}
			if truncated == nil {
				t.Fatal("fixture did not reproduce header-only bitmap")
			}
			sf := sfNew(t, &sfMemory{rows: map[string]models.StoryFlowProject{}})
			sf.media.imageOverride = truncated
			p := sf.project(t, "POST", "/projects", sf.input)
			p = sf.action(t, p, "story", nil, "")
			p = sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Stage == "story_review" })
			p = sf.action(t, p, "storyboard", []req.StoryFlowSceneShots{{SceneIndex: 1, ShotCount: 2}, {SceneIndex: 2, ShotCount: 1}}, "")
			p = sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Status == "failed" })
			if p.Stage != "storyboard" || p.FailureCode != "IMAGE_INVALID_OUTPUT" || p.CompletedImages != 0 || p.Scenes[0].Shots[0].ImageURL != nil || len(sf.video.requests) != 0 {
				t.Fatal("truncated bitmap became ready or created video")
			}
			status, _ := sf.http(t, "POST", "/projects/"+p.ID+"/video", req.StoryFlowActionRequest{Revision: 1, IdempotencyKey: uuid.NewString()}, "")
			if status != 409 {
				t.Fatal("video accepted corrupt storyboard")
			}
		})
	}
}
func TestStoryFlowDefinitiveAnalysisFailureExplicitImmutableRetry(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"story", "shots"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			sf := sfNew(t, &sfMemory{rows: map[string]models.StoryFlowProject{}})
			p := sf.project(t, "POST", "/projects", sf.input)
			if stage == "shots" {
				p = sf.action(t, p, "story", nil, "")
				p = sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Stage == "story_review" })
			}
			sf.analysis.failure = &modelark.StoryAnalysisError{Code: "ANALYSIS_REJECTED", Message: "บริการวิเคราะห์ปฏิเสธคำขอนี้", Definitive: true}
			if stage == "story" {
				p = sf.action(t, p, "story", nil, "")
			} else {
				p = sf.action(t, p, "storyboard", []req.StoryFlowSceneShots{{SceneIndex: 1, ShotCount: 2}, {SceneIndex: 2, ShotCount: 1}}, "")
			}
			p = sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Status == "failed" })
			failedCall := sf.analysis.calls - 1
			if !p.CanRetry || p.FailureCode != "ANALYSIS_REJECTED" || p.Stage != stage || sf.media.images != 0 {
				t.Fatal("definitive failure was not safely retryable")
			}
			sf.analysis.failure = nil
			p = sf.action(t, p, "retry", nil, "")
			wanted := "story_review"
			if stage == "shots" {
				wanted = "storyboard_review"
			}
			p = sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Stage == wanted })
			if sf.analysis.prompts[failedCall] != sf.analysis.prompts[failedCall+1] || sf.analysis.models[failedCall] != sf.analysis.models[failedCall+1] {
				t.Fatal("explicit analysis retry mutated frozen prompt or model")
			}
		})
	}
}
func TestStoryFlowAmbiguousAnalysisFailureNeedsAttention(t *testing.T) {
	t.Parallel()
	sf := sfNew(t, &sfMemory{rows: map[string]models.StoryFlowProject{}})
	sf.analysis.failure = errors.New("ambiguous network outcome")
	p := sf.project(t, "POST", "/projects", sf.input)
	p = sf.action(t, p, "story", nil, "")
	p = sf.until(t, p.ID, func(p *dto.StoryFlowProjectDTO) bool { return p.Status == "needs_attention" })
	if p.CanRetry || sf.analysis.calls != 1 {
		t.Fatal("ambiguous analysis retryable")
	}
	_ = sf.f.Tick(context.Background())
	if sf.analysis.calls != 1 {
		t.Fatal("ambiguous analysis automatically resubmitted")
	}
}
