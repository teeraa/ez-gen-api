package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type veTestStore struct {
	mu        sync.Mutex
	files     map[string][]byte
	server    *httptest.Server
	downloads []string
}

func newVETestStore(t *testing.T) *veTestStore {
	t.Helper()
	s := &veTestStore{files: map[string][]byte{}}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		b, ok := s.files[r.URL.Query().Get("key")]
		s.mu.Unlock()
		if !ok {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(s.server.Close)
	return s
}
func (s *veTestStore) Upload(_ context.Context, key string, data []byte, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[key] = append([]byte(nil), data...)
	return nil
}
func (s *veTestStore) SignedURL(_ context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	return s.server.URL + "/?key=" + url.QueryEscape(key), time.Now().Add(ttl), nil
}
func (s *veTestStore) SignedDownloadURL(c context.Context, key string, ttl time.Duration, _ string) (string, time.Time, error) {
	s.mu.Lock()
	s.downloads = append(s.downloads, key)
	s.mu.Unlock()
	return s.SignedURL(c, key, ttl)
}
func veTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return veTestDBWith(t, "021_video_editor.sql", "024_video_editor_audio_text.sql")
}

// veTestDBWith creates a throwaway schema with only the listed migrations.
func veTestDBWith(t *testing.T, migrations ...string) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("VIDEO_EDITOR_TEST_DSN")
	if dsn == "" {
		t.Skip("requires explicit isolated VIDEO_EDITOR_TEST_DSN")
	}
	u, e := url.Parse(dsn)
	if e != nil || u.Hostname() != "127.0.0.1" || u.Path != "/ai_editor_phase1" {
		t.Fatal("refusing non-isolated database")
	}
	config := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	admin, e := gorm.Open(postgres.Open(dsn), config)
	if e != nil {
		t.Fatal(e)
	}
	schema := "ve_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if e = admin.Exec("CREATE SCHEMA " + schema).Error; e != nil {
		t.Fatal(e)
	}
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, e := gorm.Open(postgres.Open(u.String()), config)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		sqlAdmin, _ := admin.DB()
		_ = sqlAdmin.Close()
	})
	if e = db.Exec(`CREATE TABLE users(id uuid PRIMARY KEY,created_at timestamptz DEFAULT now(),updated_at timestamptz DEFAULT now(),deleted_at timestamptz);`).Error; e != nil {
		t.Fatal(e)
	}
	for _, file := range migrations {
		migration, e := os.ReadFile("../../../migrations/" + file)
		if e != nil {
			t.Fatal(e)
		}
		if e = db.Exec(string(migration)).Error; e != nil {
			t.Fatal(e)
		}
	}
	return db
}
func veTestUser(t *testing.T, db *gorm.DB) string {
	t.Helper()
	id := uuid.NewString()
	if e := db.Exec("INSERT INTO users(id) VALUES(?)", id).Error; e != nil {
		t.Fatal(e)
	}
	return id
}
func veFixture(t *testing.T) []byte {
	t.Helper()
	for _, b := range []string{"ffmpeg", "ffprobe"} {
		if _, e := exec.LookPath(b); e != nil {
			t.Skip("requires FFmpeg")
		}
	}
	path := filepath.Join(t.TempDir(), "source.mp4")
	e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=160x120:d=1", "-f", "lavfi", "-i", "color=c=blue:s=160x120:d=1", "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-filter_complex", "[0:v][1:v]concat=n=2:v=1:a=0[v]", "-map", "[v]", "-map", "2:a", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-threads", "1", "-y", path).Run()
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestVideoEditorPersistenceSnapshotAndVolume(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := veTestDB(t)
	r := repo.NewVideoEditorRepository(db)
	store := newVETestStore(t)
	f := NewVideoEditorFacade(r, store, videoprocessing.NewFFmpegProcessor(""))
	user := veTestUser(t, db)
	other := veTestUser(t, db)
	create := req.CreateVideoEditorProject{RequestID: uuid.NewString(), Title: "ตัดต่อจริง"}
	p, created, e := f.CreateProject(ctx, user, create)
	if e != nil || !created {
		t.Fatal(e)
	}
	replay, newly, e := f.CreateProject(ctx, user, create)
	if e != nil || newly || replay.ID != p.ID {
		t.Fatal("project replay", e)
	}
	create.Title = "changed"
	if _, _, e = f.CreateProject(ctx, user, create); !errors.Is(e, repo.ErrVEIdempotency) {
		t.Fatal("project idempotency", e)
	}
	if _, e = f.Project(ctx, p.ID, other); !errors.Is(e, repo.ErrNotFound) {
		t.Fatal("ownership", e)
	}
	video := veFixture(t)
	upload := uuid.NewString()
	a, created, e := f.Upload(ctx, p.ID, user, upload, "source.mp4", "video/mp4", video)
	if e != nil || !created || a.Duration != 2 || len(a.Frames) == 0 {
		t.Fatal("upload", a, e)
	}
	again, created, e := f.Upload(ctx, p.ID, user, upload, "source.mp4", "video/mp4", video)
	if e != nil || created || again.ID != a.ID {
		t.Fatal("upload replay", e)
	}
	changed := append(append([]byte(nil), video...), byte(0))
	if _, _, e = f.Upload(ctx, p.ID, user, upload, "source.mp4", "video/mp4", changed); !errors.Is(e, repo.ErrVEIdempotency) {
		t.Fatal("upload conflict", e)
	}
	segments := []models.VideoEditorSegment{{ID: uuid.NewString(), AssetID: a.ID, Start: 0, End: .5, Volume: 0}, {ID: uuid.NewString(), AssetID: a.ID, Start: 1, End: 1.5, Volume: 1}}
	save := req.SaveVideoEditorProject{Revision: p.Revision, Title: p.Title, AspectRatio: "16:9", Segments: segments}
	saved, e := f.SaveProject(ctx, p.ID, user, save)
	if e != nil || saved.Revision != 2 || len(saved.Segments) != 2 {
		t.Fatal("save", saved, e)
	}
	if _, e = f.SaveProject(ctx, p.ID, user, save); !errors.Is(e, repo.ErrVERevision) {
		t.Fatal("stale revision", e)
	}
	exportRequest := req.CreateVideoEditorExport{RequestID: uuid.NewString(), Revision: 2, Resolution: "720p"}
	exp, created, e := f.CreateExport(ctx, p.ID, user, exportRequest)
	if e != nil || !created {
		t.Fatal(e)
	}
	save.Revision = 2
	save.AspectRatio = "1:1"
	save.Segments = segments[:1]
	save.Segments[0].End = .25
	if _, e = f.SaveProject(ctx, p.ID, user, save); e != nil {
		t.Fatal(e)
	}
	replayed, created, e := f.CreateExport(ctx, p.ID, user, exportRequest)
	if e != nil || created || replayed.ID != exp.ID {
		t.Fatal("snapshot replay after revision changed", e)
	}
	if _, e = f.ProcessNext(ctx); e != nil {
		t.Fatal("render", e)
	}
	done, e := r.Export(ctx, exp.ID, user)
	if e != nil || done.Status != "completed" || done.Duration != 1 || done.AspectRatio != "16:9" {
		t.Fatal("snapshot completion", done, e)
	}
	store.mu.Lock()
	data := append([]byte(nil), store.files[done.OutputKey]...)
	store.mu.Unlock()
	path := filepath.Join(t.TempDir(), "result.mp4")
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if out, e := exec.Command("ffmpeg", "-v", "error", "-xerror", "-i", path, "-f", "null", "-").CombinedOutput(); e != nil {
		t.Fatalf("decode %v %s", e, out)
	}
	for _, tc := range []struct {
		start string
		mute  bool
	}{{"0", true}, {"0.6", false}} {
		out, e := exec.Command("ffmpeg", "-hide_banner", "-ss", tc.start, "-t", "0.25", "-i", path, "-af", "volumedetect", "-f", "null", "-").CombinedOutput()
		if e != nil {
			t.Fatal(e)
		}
		text := string(out)
		if tc.mute && !strings.Contains(text, "mean_volume: -91.0") {
			t.Fatalf("muted segment not silent: %s", text)
		}
		if !tc.mute && strings.Contains(text, "mean_volume: -91.0") {
			t.Fatal("audible segment muted")
		}
	}
	if download, e := f.Download(ctx, exp.ID, user); e != nil || !strings.HasSuffix(download.Filename, ".mp4") {
		t.Fatal("download attachment", e)
	}
	store.mu.Lock()
	signedDownloads := append([]string(nil), store.downloads...)
	store.mu.Unlock()
	if len(signedDownloads) != 1 || signedDownloads[0] != done.OutputKey {
		t.Fatal("download must use attachment signing")
	}
	if _, e = f.Download(ctx, exp.ID, other); !errors.Is(e, repo.ErrNotFound) {
		t.Fatal("download ownership", e)
	}
}
func TestVideoEditorRevisionRaceAndLeaseFences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := veTestDB(t)
	user := veTestUser(t, db)
	r := repo.NewVideoEditorRepository(db)
	store := newVETestStore(t)
	f := NewVideoEditorFacade(r, store, videoprocessing.NewFFmpegProcessor(""))
	p, _, e := f.CreateProject(ctx, user, req.CreateVideoEditorProject{RequestID: uuid.NewString(), Title: "race"})
	if e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			_, e := f.SaveProject(ctx, p.ID, user, req.SaveVideoEditorProject{Revision: 1, Title: fmt.Sprint(i), AspectRatio: "9:16", Segments: []models.VideoEditorSegment{}})
			results <- e
		}(i)
	}
	success, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		e := <-results
		if e == nil {
			success++
		} else if errors.Is(e, repo.ErrVERevision) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("race success%d conflicts%d", success, conflicts)
	}
	lease := time.Now().Add(time.Minute)
	a := models.VideoEditorAsset{UserID: uuid.MustParse(user), ProjectID: uuid.MustParse(p.ID), ClientUploadID: uuid.New(), ContentHash: "same", Name: "test", Status: "processing", SizeBytes: 100, Frames: []byte("[]"), AttemptToken: uuid.New(), LeaseUntil: &lease}
	if _, e = r.ReserveAsset(ctx, &a); e != nil {
		t.Fatal(e)
	}
	old := a
	if _, e = r.ReserveAsset(ctx, &a); !errors.Is(e, repo.ErrVEUploadProgress) {
		t.Fatal("active upload replay", e)
	}
	if e = db.Model(&models.VideoEditorAsset{}).Where("id=?", a.ID).Update("lease_until", time.Now().Add(-time.Second)).Error; e != nil {
		t.Fatal(e)
	}
	retry := old
	retry.AttemptToken = uuid.New()
	retry.LeaseUntil = &lease
	if _, e = r.ReserveAsset(ctx, &retry); e != nil {
		t.Fatal(e)
	}
	if e = r.CompleteAsset(ctx, &old); !errors.Is(e, repo.ErrVEUploadProgress) {
		t.Fatal("stale upload completion accepted", e)
	}
	snap, _ := json.Marshal(models.VideoEditorSnapshot{})
	ex := models.VideoEditorExport{UserID: uuid.MustParse(user), ProjectID: uuid.MustParse(p.ID), RequestID: uuid.New(), RequestHash: "one", Revision: 2, Status: "queued", Resolution: "720p", AspectRatio: "9:16", Duration: 1, Snapshot: snap}
	if _, e = r.CreateExport(ctx, &ex); e != nil {
		t.Fatal(e)
	}
	claimed, e := r.Claim(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if another, e := r.Claim(ctx); !errors.Is(e, repo.ErrNotFound) {
		t.Fatal("duplicate worker claim", another, e)
	}
	stale := *claimed
	expiry := time.Now().Add(-time.Second)
	if e = db.Model(claimed).Update("lease_until", expiry).Error; e != nil {
		t.Fatal(e)
	}
	if e = r.ExpireExports(ctx); e != nil {
		t.Fatal(e)
	}
	if e = r.Retry(ctx, ex.ID.String(), user); e != nil {
		t.Fatal(e)
	}
	fresh, e := r.Claim(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.FinishExport(ctx, &stale, "old.mp4", "", ""); !errors.Is(e, repo.ErrVEExportState) {
		t.Fatal("stale export completed", e)
	}
	if e = r.FinishExport(ctx, fresh, "new.mp4", "", ""); e != nil {
		t.Fatal(e)
	}
}
func TestVideoEditorSegmentValidation(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	as := []models.VideoEditorAsset{{Base: models.Base{ID: id}, Status: "ready", Duration: 10}}
	valid := models.VideoEditorSegment{ID: uuid.NewString(), AssetID: id.String(), Start: 0, End: 2, Volume: 0}
	if e := validateVESegments([]models.VideoEditorSegment{valid}, as); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name    string
		segment models.VideoEditorSegment
	}{{"foreign", models.VideoEditorSegment{ID: valid.ID, AssetID: uuid.NewString(), Start: 0, End: 2}}, {"past duration", models.VideoEditorSegment{ID: valid.ID, AssetID: id.String(), Start: 0, End: 11}}, {"negative volume", models.VideoEditorSegment{ID: valid.ID, AssetID: id.String(), Start: 0, End: 2, Volume: -1}}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if validateVESegments([]models.VideoEditorSegment{tc.segment}, as) == nil {
				t.Fatal("accepted invalid segment")
			}
		})
	}
	if validateVESegments([]models.VideoEditorSegment{valid, valid}, as) == nil {
		t.Fatal("accepted duplicate segmentID")
	}
}

func TestVideoEditorUploadResourceCaps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := veTestDB(t)
	user := veTestUser(t, db)
	r := repo.NewVideoEditorRepository(db)
	f := NewVideoEditorFacade(r, newVETestStore(t), videoprocessing.NewFFmpegProcessor(""))
	p, _, e := f.CreateProject(ctx, user, req.CreateVideoEditorProject{RequestID: uuid.NewString(), Title: "caps"})
	if e != nil {
		t.Fatal(e)
	}
	lease := time.Now().Add(time.Minute)
	makeAsset := func() models.VideoEditorAsset {
		return models.VideoEditorAsset{UserID: uuid.MustParse(user), ProjectID: uuid.MustParse(p.ID), ClientUploadID: uuid.New(), ContentHash: "same", Name: "test.mp4", Status: "processing", SizeBytes: 100, Frames: []byte("[]"), AttemptToken: uuid.New(), LeaseUntil: &lease}
	}
	outcomes := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() { asset := makeAsset(); _, err := r.ReserveAsset(ctx, &asset); outcomes <- err }()
	}
	success, busy := 0, 0
	for i := 0; i < 3; i++ {
		err := <-outcomes
		if err == nil {
			success++
		} else if errors.Is(err, repo.ErrVEUploadBusy) {
			busy++
		} else {
			t.Fatal(err)
		}
	}
	if success != 2 || busy != 1 {
		t.Fatalf("upload cap success%d busy%d", success, busy)
	}
	if e = db.Model(&models.VideoEditorAsset{}).Where("user_id=?", user).Update("lease_until", time.Now().Add(-time.Second)).Error; e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 98; i++ {
		asset := makeAsset()
		asset.Status = "failed"
		if e = db.Create(&asset).Error; e != nil {
			t.Fatal(e)
		}
	}
	asset := makeAsset()
	if _, e = r.ReserveAsset(ctx, &asset); !errors.Is(e, repo.ErrVEUploadLimit) {
		t.Fatal("asset cap", e)
	}
}
