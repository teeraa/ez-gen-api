package facade_open_api

import (
	"context"
	"encoding/json"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"strings"
	"testing"
)

func productionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("VIDEO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires explicitly isolated VIDEO_TEST_DATABASE_URL")
	}
	if !strings.Contains(dsn, "qa") && !strings.Contains(dsn, "test") {
		t.Fatal("refusing non-test database")
	}
	db, e := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	return tx
}
func TestProductionPersistenceAndIsolation(t *testing.T) {
	db := productionTestDB(t)
	c := context.Background()
	uid := uuid.New()
	u := models.User{Email: uid.String() + "@videoqa.invalid"}
	u.ID = uid
	if e := db.Create(&u).Error; e != nil {
		t.Fatal(e)
	}
	sr := repo.NewSceneRepository(db)
	jr := repo.NewJobRepository(db)
	ur := repo.NewUploadRepository(db)
	pr := repo.NewProductionRepository(db)
	reg, _ := modelark.NewRegistry("seedance-1-0-pro-250528")
	store := storage.NewMemoryClient()
	dna := NewDNAFacade(repo.NewTagRepository(db), repo.NewCharacterProfileRepository(db), repo.NewStyleProfileRepository(db), sr, jr, store)
	jf := NewJobFacade(jr, ur, repo.NewCharacterProfileRepository(db), repo.NewStyleProfileRepository(db), sr, repo.NewTagRepository(db), modelark.NewMockClient(), reg, store, nil, 5)
	f := NewProductionFacade(pr, sr, jr, ur, dna, jf, reg, true)
	ConfigureDNAProduction(dna, f)
	ConfigureJobProduction(jf, f)
	p, e := f.Create(c, uid.String(), req.CreateProjectRequest{Brief: "character walks"})
	if e != nil {
		t.Fatalf("create project/firstscene: %v", e)
	}
	detail, e := f.Detail(c, p.ID, uid.String())
	if e != nil || len(detail.Scenes) != 1 {
		t.Fatalf("detail: %+v %v", detail, e)
	}
	sid := detail.Scenes[0].ID
	if _, e = f.Detail(c, p.ID, uuid.NewString()); e != ErrNotFound {
		t.Fatalf("foreignproject: %v", e)
	}
	copy, e := f.Duplicate(c, sid, uid.String())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Reorder(c, p.ID, uid.String(), []string{copy.ID, sid}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Reorder(c, p.ID, uid.String(), []string{sid}); e != ErrConflict {
		t.Fatalf("partialorder: %v", e)
	}
	draft := models.EditDraft{Clips: []models.EditClip{{SceneID: sid, Enabled: true}}, AudioVolume: 0, TextLayers: []models.EditTextLayer{}}
	saved, e := f.SaveEditor(c, p.ID, uid.String(), req.SaveEditorRequest{Revision: 1, EditDraft: draft})
	if e != nil || saved.Revision != 2 {
		t.Fatalf("save: %+v %v", saved, e)
	}
	if _, e = f.SaveEditor(c, p.ID, uid.String(), req.SaveEditorRequest{Revision: 1, EditDraft: draft}); e != ErrConflict {
		t.Fatalf("stale: %v", e)
	}
	// Snapshot real selected job IDs and keys; later scene changes must not mutate queued export.
	sceneUUID, _ := uuid.Parse(sid)
	key := "outputs/qa/one.mp4"
	kind := "video"
	duration := 1000
	j := &models.VideoGenerationJob{UserID: uid, SceneID: &sceneUUID, Status: models.JobStatusSucceeded, Mode: "text", ModelID: "violet-motion-2", Prompt: "walk", AspectRatio: "16:9", DurationSeconds: 5, Quality: "standard", OutputMediaKey: &key, OutputMediaType: &kind, OutputDurationMs: &duration}
	if e = db.Create(j).Error; e != nil {
		t.Fatal(e)
	}
	if e = dna.SelectSceneOutput(c, sid, j.ID.String(), uid.String()); e != nil {
		t.Fatal(e)
	}
	bad := *j
	bad.ID = uuid.New()
	bad.Mode = "text_image"
	if e = db.Create(&bad).Error; e != nil {
		t.Fatal(e)
	}
	if e = dna.SelectSceneOutput(c, sid, bad.ID.String(), uid.String()); e != ErrSceneHasNoOutput {
		t.Fatalf("image selected: %v", e)
	}
	ef := NewExportFacade(repo.NewExportRepository(db), jr, ur, sr, store, &videoprocessing.MockProcessor{})
	ConfigureExportProduction(ef, pr)
	idem := uuid.NewString()
	in := CreateExportInput{ProjectID: &p.ID, IdempotencyKey: &idem, AudioVolume: 0, Clips: []ExportClip{{SceneID: sid, TrimEnd: 0.5}}}
	ex, e := ef.CreateExport(c, uid.String(), in)
	if e != nil {
		t.Fatal(e)
	}
	again, e := ef.CreateExport(c, uid.String(), in)
	if e != nil || ex.ExportID != again.ExportID {
		t.Fatalf("duplicate: %v", e)
	}
	in.Clips[0].TrimEnd = 0.7
	if _, e = ef.CreateExport(c, uid.String(), in); e != ErrConflict {
		t.Fatalf("mismatch: %v", e)
	}
	var stored models.StoryboardExport
	db.First(&stored, "id=?", ex.ExportID)
	var clips []models.StoredClip
	_ = json.Unmarshal(stored.Clips, &clips)
	if len(clips) != 1 || clips[0].JobID != j.ID.String() || clips[0].StorageKey != key || stored.AudioVolume != 0 {
		t.Fatalf("snapshot: %+v", stored)
	}
	if e = f.Delete(c, p.ID, uid.String()); e != nil {
		t.Fatal(e)
	}
	if _, e = dna.GetScene(c, sid, uid.String()); e != ErrNotFound {
		t.Fatalf("deletedproject scene: %v", e)
	}
}
