package tests

import (
	"context"
	"fmt"
	"testing"
	"time"

	facade "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	handler "github.com/ez-gen/ez-gen-api/apps/handlers/handler_open_api"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func directorAdmissionPointer[T any](v T) *T { return &v }
func directorAdmissionApp(d *directorFixture, rate int) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("userID", d.user)
		c.Locals("requestID", "director-admission-fixture")
		return c.Next()
	})
	handler.NewDirectorHandler(d.facade, rate).RegisterRoutes(app.Group("/open/v1"))
	d.app = app
}
func TestDirectorAdmissionAutosaveDoesNotSpendAIQuota(t *testing.T) {
	d := newDirectorFixture(t)
	directorAdmissionApp(d, 10)
	p := d.create()
	for i := 0; i < 15; i++ {
		p = d.project("PATCH", "/projects/"+p.ID, map[string]any{"revision": p.Revision, "name": fmt.Sprintf("ฉบับที่ %d", i)})
	}
	// Creation consumed one AI admission. Ordinary autosaves must consume none.
	for i := 0; i < 9; i++ {
		d.wantError("POST", "/projects/"+p.ID+"/cover", map[string]any{"revision": p.Revision, "idempotencyKey": uuid.NewString()}, 400, "VALIDATION_ERROR")
	}
	status, body, headers := d.http("POST", "/projects/"+p.ID+"/cover", map[string]any{"revision": p.Revision, "idempotencyKey": uuid.NewString()}, nil)
	if status != 429 || headers.Get("Retry-After") == "" {
		t.Fatalf("AI quota missing: %d retry=%q body=%s", status, headers.Get("Retry-After"), body)
	}
}
func TestDirectorAdmissionUsesTransactionConnectionForOwnedCharacters(t *testing.T) {
	d := newDirectorFixture(t)
	if err := d.db.Exec(`CREATE TABLE library_assets(id uuid PRIMARY KEY,user_id uuid NOT NULL,asset_type text NOT NULL,description text,storage_key text,deleted_at timestamptz)`).Error; err != nil {
		t.Fatal(err)
	}
	character := uuid.NewString()
	if err := d.db.Exec(`INSERT INTO library_assets(id,user_id,asset_type,description,storage_key) VALUES (?,?,'character','นักเดินทาง','characters/fixture.png')`, character, d.user).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := modelark.NewRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	d.facade = facade.NewDirectorFacade(d.store, repo.NewStudioRepository(d.db), d.media, d.analyzer, d.media, registry, nil, nil, facade.DirectorOptions{Enabled: true, AnalysisModel: "fixture-story", ImageModel: "fixture-image", StoryPoints: 4, ImagePoints: 3, VideoPoints: 20, SampleBaseURL: "https://samples.test/director"})
	directorAdmissionApp(d, 1000)
	p := d.create()
	d.fund(30)
	pool, err := d.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	idea := req.DirectorRequest{Revision: &p.Revision, Prompt: directorAdmissionPointer("นักเดินทางค้นหาดวงดาวที่หายไป"), Genre: directorAdmissionPointer("adventure"), Moods: directorAdmissionPointer([]string{"อบอุ่น"}), SceneCount: directorAdmissionPointer(1), CharacterAssetIDs: directorAdmissionPointer([]string{character}), StoryModelID: directorAdmissionPointer("director-story")}
	p, err = d.facade.Mutate(ctx, p.ID, d.user, "idea", "", idea)
	if err != nil {
		t.Fatalf("owned character autosave must use transaction connection, not wait for a second pooled connection: %v (ctx=%v)", err, ctx.Err())
	}
	key := uuid.NewString()
	cover := req.DirectorRequest{Revision: &p.Revision, IdempotencyKey: key}
	accepted, err := d.facade.Mutate(ctx, p.ID, d.user, "cover-generate", "", cover)
	if err != nil {
		t.Fatalf("AI snapshot must use transaction connection: %v (ctx=%v)", err, ctx.Err())
	}
	if accepted.Cover == nil {
		t.Fatal("cover not queued")
	}
	if err = d.db.Exec("DELETE FROM library_assets WHERE id=?", character).Error; err != nil {
		t.Fatal(err)
	}
	replay, err := d.facade.Mutate(ctx, p.ID, d.user, "cover-generate", "", cover)
	if err != nil || replay.Cover == nil || replay.Cover.ID != accepted.Cover.ID {
		t.Fatalf("replay must precede deleted asset revalidation: %#v %v", replay.Cover, err)
	}
	if got := d.balance(); got != 27 {
		t.Fatalf("duplicate charged points: %d", got)
	}
}
