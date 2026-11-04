package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	facade "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	handler "github.com/ez-gen/ez-gen-api/apps/handlers/handler_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type directorFixture struct {
	t        *testing.T
	db       *gorm.DB
	store    *repo.DirectorRepository
	facade   *facade.DirectorFacade
	app      *fiber.App
	user     string
	media    *directorTestMedia
	analyzer *directorTestAnalyzer
}
type directorTestAssets struct{}

func (directorTestAssets) Asset(context.Context, string, string) (*models.LibraryAsset, error) {
	return nil, repo.ErrNotFound
}

type directorTestMedia struct {
	mu            sync.Mutex
	calls         int
	failure       error
	invalid       bool
	uploadFailure bool
	entered       chan struct{}
	release       chan struct{}
	objects       map[string][]byte
	requests      []modelark.ImageRequest
	rejectRefs    error
}

func (m *directorTestMedia) Upload(_ context.Context, key string, data []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.uploadFailure {
		return errors.New("fixture storage unavailable")
	}
	m.objects[key] = bytes.Clone(data)
	return nil
}
func (m *directorTestMedia) SignedURL(_ context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	return "https://owned.test/" + url.PathEscape(key), time.Now().UTC().Add(ttl), nil
}
func (m *directorTestMedia) GenerateImage(ctx context.Context, r modelark.ImageRequest) ([]byte, error) {
	m.mu.Lock()
	m.calls++
	m.requests = append(m.requests, r)
	if m.rejectRefs != nil && len(r.ReferenceURLs) > 0 {
		e := m.rejectRefs
		m.mu.Unlock()
		return nil, e
	}
	failure, entered, release, invalid := m.failure, m.entered, m.release, m.invalid
	m.entered = nil
	m.release = nil
	m.mu.Unlock()
	if entered != nil {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if failure != nil {
		return nil, failure
	}
	if invalid {
		return []byte("invalid image fixture"), nil
	}
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 160, 90))); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func (m *directorTestMedia) count() int { m.mu.Lock(); defer m.mu.Unlock(); return m.calls }
func (m *directorTestMedia) block() (chan struct{}, chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entered = make(chan struct{})
	m.release = make(chan struct{})
	return m.entered, m.release
}

type directorTestAnalyzer struct {
	mu    sync.Mutex
	calls int
	reply func(string) (string, error)
}

func (a *directorTestAnalyzer) Analyze(_ context.Context, _ string, prompt string) (string, error) {
	a.mu.Lock()
	a.calls++
	fn := a.reply
	a.mu.Unlock()
	if fn == nil {
		return "", errors.New("test planner reply not configured")
	}
	return fn(prompt)
}

func newDirectorFixture(t *testing.T) *directorFixture {
	t.Helper()
	return newDirectorFixtureWith(t, nil, nil)
}

type directorVideoFetcher interface {
	Fetch(context.Context, string) ([]byte, string, error)
}

func newDirectorFixtureWith(t *testing.T, video modelark.Client, fetcher directorVideoFetcher) *directorFixture {
	t.Helper()
	dsn := os.Getenv("DIRECTOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("DIRECTOR_TEST_DATABASE_URL unset; disposable loopback fixture only")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Port() != "55439" || u.User == nil || u.User.Username() != "director_test" || u.Path != "/postgres" {
		t.Fatal("refusing nonfixture Director database")
	}
	if (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Fragment != "" {
		t.Fatal("refusing nonfixture Director database scheme")
	}
	for key, values := range u.Query() {
		if key != "sslmode" || len(values) != 1 || values[0] != "disable" {
			t.Fatal("refusing Director database connection overrides")
		}
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	schema := "director_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err = base.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
		if e := base.Exec("DROP SCHEMA " + schema + " CASCADE").Error; e != nil {
			t.Errorf("cleanup: %v", e)
		}
		s, _ := base.DB()
		_ = s.Close()
	})
	if err = db.Exec("CREATE TABLE users(id uuid PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "029_director.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	user := uuid.NewString()
	if err = db.Exec("INSERT INTO users(id) VALUES (?)", user).Error; err != nil {
		t.Fatal(err)
	}
	store := repo.NewDirectorRepository(db, 3)
	media := &directorTestMedia{objects: map[string][]byte{}}
	analyzer := &directorTestAnalyzer{}
	registry, err := modelark.NewRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	f := facade.NewDirectorFacade(store, directorTestAssets{}, media, analyzer, media, registry, video, fetcher, facade.DirectorOptions{Enabled: true, AnalysisModel: "fixture-story", ImageModel: "fixture-image", StoryPoints: 4, ImagePoints: 3, VideoPoints: 20, DraftPoints: 8, SampleBaseURL: "https://samples.test/director", VideoEnabled: video != nil})
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		owner := c.Get("X-Test-User")
		if owner == "" {
			owner = user
		}
		c.Locals("userID", owner)
		c.Locals("requestID", "director-fixture")
		return c.Next()
	})
	handler.NewDirectorHandler(f, 1000).RegisterRoutes(app.Group("/open/v1"))
	return &directorFixture{t, db, store, f, app, user, media, analyzer}
}
func (d *directorFixture) http(method, path string, body any, headers map[string]string) (int, []byte, http.Header) {
	d.t.Helper()
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			d.t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, "/open/v1/director"+path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	res, err := d.app.Test(r, -1)
	if err != nil {
		d.t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		d.t.Fatal(err)
	}
	return res.StatusCode, b, res.Header
}
func (d *directorFixture) project(method, path string, body any) dto.DirectorProject {
	d.t.Helper()
	status, b, _ := d.http(method, path, body, nil)
	if status < 200 || status >= 300 {
		d.t.Fatalf("%s %s: %d %s", method, path, status, b)
	}
	var out struct {
		Data dto.DirectorProject `json:"data"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		d.t.Fatal(err)
	}
	if out.Data.ID == "" {
		d.t.Fatalf("missing project: %s", b)
	}
	return out.Data
}
func (d *directorFixture) wantError(method, path string, body any, status int, code string) {
	d.t.Helper()
	got, b, _ := d.http(method, path, body, nil)
	var out struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(b, &out)
	if got != status || out.Code != code {
		d.t.Fatalf("%s %s: want %d %s got %d %s", method, path, status, code, got, b)
	}
}
func (d *directorFixture) create() dto.DirectorProject {
	return d.project("POST", "/projects", map[string]any{"name": "การเดินทางของดาว", "aspectRatio": "16:9", "style": "cinematic", "resolution": "1080p", "idempotencyKey": uuid.NewString()})
}
func (d *directorFixture) idea(p dto.DirectorProject) dto.DirectorProject {
	return d.project("PUT", "/projects/"+p.ID+"/idea", map[string]any{"revision": p.Revision, "prompt": "เด็กน้อยออกเดินทางตามหาดวงดาวที่หายไป", "genre": "adventure", "moods": []string{"อบอุ่น"}, "sceneCount": 1, "characterAssetIds": []string{}, "storyModelId": "director-story"})
}
func (d *directorFixture) fund(points int) {
	d.t.Helper()
	if e := d.db.Exec("INSERT INTO director_point_accounts(user_id,balance) VALUES (?,?) ON CONFLICT(user_id) DO UPDATE SET balance=EXCLUDED.balance", d.user, points).Error; e != nil {
		d.t.Fatal(e)
	}
}
func (d *directorFixture) balance() int64 {
	d.t.Helper()
	var balance int64
	if e := d.db.Raw("SELECT balance FROM director_point_accounts WHERE user_id=?", d.user).Scan(&balance).Error; e != nil {
		d.t.Fatal(e)
	}
	return balance
}
func (d *directorFixture) tick() {
	d.t.Helper()
	if err := d.facade.Tick(context.Background()); err != nil {
		d.t.Fatal(err)
	}
}
func (d *directorFixture) get(id string) dto.DirectorProject {
	return d.project("GET", "/projects/"+id, nil)
}
func (d *directorFixture) action(p dto.DirectorProject, suffix string) dto.DirectorProject {
	return d.project("POST", "/projects/"+p.ID+suffix, map[string]any{"revision": p.Revision, "idempotencyKey": uuid.NewString()})
}

func TestDirectorPostgresHTTPRevisionIdempotencyBilling(t *testing.T) {
	d := newDirectorFixture(t)
	p := d.idea(d.create())
	path := "/projects/" + p.ID
	status, b, _ := d.http("GET", path, nil, map[string]string{"X-Test-User": uuid.NewString()})
	if status != 404 || !bytes.Contains(b, []byte("PROJECT_NOT_FOUND")) {
		t.Fatalf("tenant: %d %s", status, b)
	}
	d.wantError("PATCH", path, map[string]any{"revision": p.Revision - 1, "name": "ใหม่"}, 409, "REVISION_CONFLICT")
	d.wantError("POST", path+"/cover", map[string]any{"revision": p.Revision, "idempotencyKey": uuid.NewString()}, 402, "INSUFFICIENT_POINTS")
	d.fund(100)
	key := uuid.NewString()
	in := map[string]any{"revision": p.Revision, "idempotencyKey": key}
	queued := d.project("POST", path+"/cover", in)
	if queued.Cover == nil || queued.Cover.Status != "queued" || d.balance() != 97 {
		t.Fatalf("cover admission: %+v balance %d", queued.Cover, d.balance())
	}
	renamed := d.project("PATCH", path, map[string]any{"revision": queued.Revision, "name": "ชื่อใหม่"})
	if renamed.Revision != queued.Revision+1 {
		t.Fatal("user edit did not increment revision")
	}
	replay := d.project("POST", path+"/cover", in)
	if replay.Cover.ID != queued.Cover.ID || d.balance() != 97 {
		t.Fatal("replay charged/created twice")
	}
	d.wantError("POST", path+"/cover", map[string]any{"revision": renamed.Revision, "idempotencyKey": key}, 409, "IDEMPOTENCY_CONFLICT")
	_, _, before := d.http("GET", path, nil, nil)
	d.tick()
	done := d.get(p.ID)
	if done.Name != "ชื่อใหม่" || done.Revision != renamed.Revision || done.Cover.Status != "succeeded" || done.Cover.ImageURL == nil || done.Cover.ExpiresAt == nil {
		t.Fatalf("completion overwrote user change or missing media: %+v", done)
	}
	_, _, after := d.http("GET", path, nil, nil)
	if before.Get("ETag") == "" || before.Get("ETag") == after.Get("ETag") {
		t.Fatal("worker did not change ETag")
	}
	status, b, _ = d.http("GET", path, nil, map[string]string{"If-None-Match": after.Get("ETag")})
	if status != 304 || len(b) != 0 {
		t.Fatalf("conditional GET %d %s", status, b)
	}
	d.wantError("POST", path+"/video", map[string]any{"revision": done.Revision, "idempotencyKey": uuid.NewString()}, 503, "DIRECTOR_VIDEO_UNAVAILABLE")
	status, b, _ = d.http("DELETE", path, nil, nil)
	if status != 204 {
		t.Fatalf("delete %d %s", status, b)
	}
	d.wantError("GET", path, nil, 404, "PROJECT_NOT_FOUND")
}

func directorPlanner(prompt string) (string, error) {
	if strings.Contains(prompt, "DIRECTOR_STORY") {
		return `{"logline":"เด็กน้อยตามหาดาวจนค้นพบความกล้า","scenes":[{"title":"ดาวที่หายไป","summary":"เด็กน้อยออกเดินทางผ่านป่าและค้นพบดวงดาว","mood":"อบอุ่น","characterAssetIds":[]}]}`, nil
	}
	if strings.Contains(prompt, "DIRECTOR_REWRITE") {
		return `{"title":"ดาวดวงใหม่","summary":"เด็กน้อยพบเพื่อนร่วมทางและช่วยกันตามหาดาว","mood":"อบอุ่น","characterAssetIds":[]}`, nil
	}
	if strings.Contains(prompt, "DIRECTOR_ADD") {
		return `{"description":"เด็กน้อยหันไปมองดาวที่ปลายทาง","camera":"มุมกว้าง","characterAssetIds":[]}`, nil
	}
	if strings.Contains(prompt, "DIRECTOR_PLAN") {
		marker := "Project and instruction data: "
		start := strings.LastIndex(prompt, marker)
		if start >= 0 {
			start += len(marker)
		}
		if start < 0 {
			return "", errors.New("missing snapshot in fixture planner prompt")
		}
		var p dto.DirectorProject
		if err := json.Unmarshal([]byte(prompt[start:]), &p); err != nil {
			return "", fmt.Errorf("fixture snapshot: %w", err)
		}
		if p.ID == "" {
			var wrapper struct {
				Project dto.DirectorProject `json:"project"`
			}
			if err := json.Unmarshal([]byte(prompt[start:]), &wrapper); err != nil {
				return "", err
			}
			p = wrapper.Project
		}
		type plannedShot struct {
			ID          string   `json:"id"`
			Description string   `json:"description"`
			Camera      string   `json:"camera"`
			Characters  []string `json:"characterAssetIds"`
		}
		out := struct {
			Shots []plannedShot `json:"shots"`
		}{Shots: []plannedShot{}}
		for _, sc := range p.Scenes {
			for _, sh := range sc.Shots {
				out.Shots = append(out.Shots, plannedShot{sh.ID, "เด็กน้อยเดินทางผ่านป่าเพื่อมองหาดวงดาว", "มุมกว้าง", []string{}})
			}
		}
		b, err := json.Marshal(out)
		return string(b), err
	}
	return "", errors.New("unrecognized fixture planner operation")
}
func (d *directorFixture) board() dto.DirectorProject {
	d.t.Helper()
	d.analyzer.reply = directorPlanner
	d.fund(100)
	p := d.idea(d.create())
	p = d.action(p, "/story")
	if len(p.Scenes) != 1 || p.Scenes[0].Status != "pending" {
		d.t.Fatalf("missing scene placeholder: %+v", p.Scenes)
	}
	revision := p.Revision
	d.tick()
	p = d.get(p.ID)
	if p.Story.Status != "succeeded" || p.Scenes[0].Status != "ready" || p.Revision != revision {
		d.t.Fatalf("story failed/worker revision changed: %+v", p.Story)
	}
	p = d.project("PATCH", "/projects/"+p.ID+"/scenes/"+p.Scenes[0].ID, map[string]any{"revision": p.Revision, "shotCount": 2})
	p = d.action(p, "/storyboard")
	if len(p.Scenes[0].Shots) != 2 || p.Scenes[0].Shots[0].Image.Status != "pending" {
		d.t.Fatal("missing shot placeholders")
	}
	for i := 0; i < 8 && p.Storyboard.Status != "succeeded"; i++ {
		d.tick()
		p = d.get(p.ID)
	}
	if p.Storyboard.Status != "succeeded" || p.Storyboard.CompletedImages != 2 {
		d.t.Fatalf("board not complete: %+v", p.Storyboard)
	}
	return p
}
func TestDirectorPostgresHTTPStoryboardEditsAndCover(t *testing.T) {
	d := newDirectorFixture(t)
	p := d.board()
	path := "/projects/" + p.ID
	scene := p.Scenes[0].ID
	shot := p.Scenes[0].Shots[0].ID
	d.wantError("POST", path+"/storyboard", map[string]any{"revision": p.Revision, "idempotencyKey": uuid.NewString()}, 409, "STORYBOARD_EXISTS")
	p = d.project("PUT", path+"/cover", map[string]any{"revision": p.Revision, "shotId": shot})
	if p.Cover == nil || p.Cover.Source != "shot" || p.CoverImageURL == nil {
		t.Fatal("shot cover missing")
	}
	p = d.project("PATCH", path+"/shots/"+shot, map[string]any{"revision": p.Revision, "description": "เด็กน้อยยกมือต้อนรับแสงดาว"})
	if !p.Scenes[0].Shots[0].Image.Stale {
		t.Fatal("completed image edit not stale")
	}
	oldURL := *p.Scenes[0].Shots[0].Image.URL
	p = d.action(p, "/shots/"+shot+"/regenerate")
	if p.Scenes[0].Shots[0].Image.URL == nil || *p.Scenes[0].Shots[0].Image.URL != oldURL {
		t.Fatal("regenerate discarded previous image")
	}
	d.tick()
	p = d.get(p.ID)
	if p.Scenes[0].Shots[0].Image.Stale {
		t.Fatal("regenerated image still stale")
	}
	p = d.action(p, "/scenes/"+scene+"/shots")
	if len(p.Scenes[0].Shots) != 3 {
		t.Fatal("add placeholder absent")
	}
	for i := 0; i < 4 && p.Storyboard.Status != "succeeded"; i++ {
		d.tick()
		p = d.get(p.ID)
	}
	if len(p.Scenes[0].Shots) != 3 || p.Scenes[0].Shots[2].Image.Status != "succeeded" {
		t.Fatal("added shot did not complete")
	}
	p = d.project("DELETE", fmt.Sprintf("%s/shots/%s?revision=%d", path, shot, p.Revision), nil)
	if p.Cover != nil || len(p.Scenes[0].Shots) != 2 || p.Scenes[0].Shots[0].Index != 1 {
		t.Fatal("delete did not clear cover/reindex")
	}
	last := p.Scenes[0].Shots[0].ID
	p = d.project("DELETE", fmt.Sprintf("%s/shots/%s?revision=%d", path, last, p.Revision), nil)
	d.wantError("DELETE", fmt.Sprintf("%s/shots/%s?revision=%d", path, p.Scenes[0].Shots[0].ID, p.Revision), nil, 409, "LAST_SHOT")
	p = d.project("PATCH", path, map[string]any{"revision": p.Revision, "step": "plot"})
	revision := p.Revision
	p = d.project("PATCH", path, map[string]any{"revision": revision, "step": "storyboard"})
	if p.Revision != revision {
		t.Fatal("navigation increased revision")
	}
}
func TestDirectorPostgresHTTPInFlightEditPreservedStale(t *testing.T) {
	d := newDirectorFixture(t)
	p := d.board()
	path := "/projects/" + p.ID
	shot := p.Scenes[0].Shots[0].ID
	p = d.action(p, "/shots/"+shot+"/regenerate")
	entered, release := d.media.block()
	done := make(chan error, 1)
	go func() { done <- d.facade.Tick(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider never entered")
	}
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	p = d.project("PATCH", path+"/shots/"+shot, map[string]any{"revision": p.Revision, "description": "ข้อความแก้ขณะภาพกำลังสร้าง"})
	revision := p.Revision
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker never completed")
	}
	p = d.get(p.ID)
	sh := p.Scenes[0].Shots[0]
	if sh.Description != "ข้อความแก้ขณะภาพกำลังสร้าง" || !sh.Image.Stale || sh.Image.Status != "succeeded" || p.Revision != revision {
		t.Fatalf("completion lost edit/stale: %+v revision%d", sh, p.Revision)
	}
}
func TestDirectorPostgresExpiredLeaseNeverResubmits(t *testing.T) {
	d := newDirectorFixture(t)
	d.fund(100)
	p := d.idea(d.create())
	p = d.action(p, "/cover")
	first, err := d.store.Claim(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = d.db.Model(&models.DirectorJob{}).Where("id=?", first.ID).Update("lease_until", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	d.tick()
	p = d.get(p.ID)
	if d.media.count() != 0 || p.Cover.Status != "needs_attention" || p.Cover.CanRetry {
		t.Fatalf("uncertain request resubmitted/retryable: %+v calls%d", p.Cover, d.media.count())
	}
	err = d.store.Complete(context.Background(), first, "succeeded", false, func(*models.DirectorProject) error { return errors.New("stale completion callback must never execute") })
	if err == nil {
		t.Fatal("stale lease accepted")
	}
	if d.balance() != 97 {
		t.Fatal("uncertain charge incorrectly refunded")
	}
}
func TestDirectorPostgresDefinitiveStoryFailureRefundOnce(t *testing.T) {
	d := newDirectorFixture(t)
	d.fund(100)
	d.analyzer.reply = func(string) (string, error) {
		return "", &modelark.StoryAnalysisError{Code: "ANALYSIS_REJECTED", Message: "private provider diagnostic must not escape", Definitive: true}
	}
	p := d.idea(d.create())
	p = d.action(p, "/story")
	if d.balance() != 96 {
		t.Fatal("story charge absent")
	}
	d.tick()
	p = d.get(p.ID)
	if p.Story.Status != "failed" || p.Story.PointsRefunded != 4 || d.balance() != 100 {
		t.Fatalf("failed story not refunded %+v balance%d", p.Story, d.balance())
	}
	if p.Story.FailureMessage != nil && strings.Contains(*p.Story.FailureMessage, "private provider") {
		t.Fatal("provider error leaked")
	}
	err := d.facade.Tick(context.Background())
	if !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("finished failure reclaimed: %v", err)
	}
	if d.balance() != 100 {
		t.Fatal("refund duplicated")
	}
	var count int64
	if err = d.db.Model(&models.DirectorPointEntry{}).Where("kind=?", "refund").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("refund ledger count %d err%v", count, err)
	}
}

func TestDirectorPostgresHTTPValidationAndSummary(t *testing.T) {
	d := newDirectorFixture(t)
	p := d.create()
	path := "/projects/" + p.ID
	d.wantError("PATCH", path, map[string]any{"revision": p.Revision, "step": "plot"}, 409, "DIRECTOR_STATE_CONFLICT")
	status, b, _ := d.http("PATCH", path, map[string]any{"revision": p.Revision, "name": strings.Repeat("ก", 81)}, nil)
	var out struct {
		Code    string `json:"code"`
		Details []struct {
			Field string `json:"field"`
		} `json:"details"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if status != 400 || out.Code != "VALIDATION_ERROR" || len(out.Details) != 1 || out.Details[0].Field != "name" {
		t.Fatalf("field validation: %d %s", status, b)
	}
	d.wantError("PATCH", path, map[string]any{"revision": p.Revision, "name": "a" + strings.Repeat(" ", 81)}, 400, "VALIDATION_ERROR")
	d.wantError("GET", "/projects?limit=51", nil, 400, "VALIDATION_ERROR")
	status, b, _ = d.http("GET", "/projects?search="+url.QueryEscape("การเดินทาง"), nil, nil)
	var list struct {
		Data dto.DirectorList `json:"data"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	if status != 200 || list.Data.Total != 1 || len(list.Data.Items) != 1 || list.Data.Items[0].SceneCount != 3 {
		t.Fatalf("summary missing current sceneCount: %d %s", status, b)
	}
	status, b, _ = d.http("GET", "/projects?search=absent", nil, nil)
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	if status != 200 || list.Data.Total != 0 || len(list.Data.Items) != 0 {
		t.Fatalf("search not applied: %d %s", status, b)
	}
}

func TestDirectorPostgresConcurrentAdmissionChargesOnce(t *testing.T) {
	d := newDirectorFixture(t)
	d.fund(100)
	p := d.idea(d.create())
	path := "/projects/" + p.ID + "/cover"
	in := map[string]any{"revision": p.Revision, "idempotencyKey": uuid.NewString()}
	type result struct {
		status int
		body   []byte
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() { <-start; s, b, _ := d.http("POST", path, in, nil); results <- result{s, b} }()
	}
	close(start)
	ids := []string{}
	for i := 0; i < 2; i++ {
		r := <-results
		var out struct {
			Data dto.DirectorProject `json:"data"`
		}
		if err := json.Unmarshal(r.body, &out); err != nil {
			t.Fatal(err)
		}
		if r.status != 200 || out.Data.Cover == nil {
			t.Fatalf("concurrent admission %d %s", r.status, r.body)
		}
		ids = append(ids, out.Data.Cover.ID)
	}
	if ids[0] != ids[1] || d.balance() != 97 {
		t.Fatalf("concurrent duplicate billed %v balance%d", ids, d.balance())
	}
	var n int64
	if err := d.db.Model(&models.DirectorJob{}).Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("duplicate jobs %d %v", n, err)
	}
}
func TestDirectorPostgresImageFailureRetryAndStorageRecovery(t *testing.T) {
	d := newDirectorFixture(t)
	p := d.board()
	shot := p.Scenes[0].Shots[0].ID
	before := d.balance()
	d.media.mu.Lock()
	d.media.invalid = true
	d.media.mu.Unlock()
	p = d.action(p, "/shots/"+shot+"/regenerate")
	d.tick()
	p = d.get(p.ID)
	if p.Scenes[0].Shots[0].Image.Status != "failed" || !p.Scenes[0].Shots[0].Image.CanRetry || p.Scenes[0].Shots[0].PointsRefunded != 3 || d.balance() != before {
		t.Fatalf("image failure refund: %+v balance%d", p.Scenes[0].Shots[0], d.balance())
	}
	d.media.mu.Lock()
	d.media.invalid = false
	d.media.uploadFailure = true
	d.media.mu.Unlock()
	p = d.action(p, "/shots/"+shot+"/retry")
	calls := d.media.count()
	if err := d.facade.Tick(context.Background()); err == nil {
		t.Fatal("storage failure not observed")
	}
	if d.media.count() != calls+1 {
		t.Fatal("image provider not called exactly once")
	}
	var j models.DirectorJob
	if err := d.db.Where("project_id=? AND status=?", p.ID, "submitting").First(&j).Error; err != nil {
		t.Fatal(err)
	}
	if err := d.db.Model(&j).Update("lease_until", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	d.media.mu.Lock()
	d.media.uploadFailure = false
	d.media.mu.Unlock()
	d.tick()
	p = d.get(p.ID)
	if d.media.count() != calls+1 || p.Scenes[0].Shots[0].Image.Status != "succeeded" || d.balance() != before-3 {
		t.Fatalf("recovery resubmitted/lost output calls%d shot%+v balance%d", d.media.count(), p.Scenes[0].Shots[0], d.balance())
	}
}
