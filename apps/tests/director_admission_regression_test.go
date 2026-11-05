package tests

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

func TestDirectorCoverLimitsReferencesAndFallsBackToText(t *testing.T) {
	d := newDirectorFixture(t)
	if err := d.db.Exec(`CREATE TABLE library_assets(id uuid PRIMARY KEY,user_id uuid NOT NULL,name text,asset_type text NOT NULL,description text,prompt text,storage_key text,deleted_at timestamptz)`).Error; err != nil {
		t.Fatal(err)
	}
	ids := []string{uuid.NewString(), uuid.NewString()}
	for i, id := range ids {
		if err := d.db.Exec(`INSERT INTO library_assets(id,user_id,name,asset_type,description,prompt,storage_key) VALUES (?,?,'ตัวละครสังเคราะห์','character',?,?,?)`, id, d.user, fmt.Sprintf("ชุดสีที่ %d", i), fmt.Sprintf("long black hair %d", i), fmt.Sprintf("characters/%d.png", i)).Error; err != nil {
			t.Fatal(err)
		}
	}
	registry, err := modelark.NewRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	d.facade = facade.NewDirectorFacade(d.store, repo.NewStudioRepository(d.db), d.media, d.analyzer, d.media, registry, nil, nil, facade.DirectorOptions{Enabled: true, AnalysisModel: "fixture-story", ImageModel: "fixture-image", StoryPoints: 4, ImagePoints: 3, VideoPoints: 20, MaxImageReferences: 1})
	directorAdmissionApp(d, 1000)
	d.fund(30)
	p := d.create()
	p = d.project("PUT", "/projects/"+p.ID+"/idea", map[string]any{"revision": p.Revision, "prompt": "สองจอมยุทธ์ประลองกระบี่", "genre": "fantasy", "moods": []string{"ตื่นเต้น"}, "sceneCount": 1, "characterAssetIds": ids, "storyModelId": "director-story"})
	d.media.rejectRefs = &modelark.ImageError{Code: "MODEL_UNAVAILABLE", Message: "fixture 404"}
	p = d.action(p, "/cover")
	d.tick()
	p = d.get(p.ID)
	if p.Cover == nil || p.Cover.Status != "succeeded" {
		t.Fatalf("cover must fall back to text-only generation: %+v", p.Cover)
	}
	if len(d.media.requests) != 2 || len(d.media.requests[0].ReferenceURLs) != 1 || len(d.media.requests[1].ReferenceURLs) != 0 {
		t.Fatalf("reference attempts: %+v", d.media.requests)
	}
	prompt := d.media.requests[1].Prompt
	for _, want := range []string{"ตัวละครสังเคราะห์ A", "ตัวละครสังเคราะห์ B", "long black hair 0", "long black hair 1"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("text identity missing %q:\n%s", want, prompt)
		}
	}
}

type directorTestDescriber struct {
	mu    sync.Mutex
	calls int
}

func (d *directorTestDescriber) DescribeImage(_ context.Context, _ string, image []byte, mime string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if mime != "image/png" || len(image) == 0 {
		return "", fmt.Errorf("unexpected image %s", mime)
	}
	return "young swordsman, navy blue robe with gold trim, white sash, black topknot", nil
}

func TestDirectorCharacterLookIsDescribedOnceAndReusedEverywhere(t *testing.T) {
	d := newDirectorFixture(t)
	if err := d.db.Exec(`CREATE TABLE library_assets(id uuid PRIMARY KEY,user_id uuid NOT NULL,name text,asset_type text NOT NULL,description text,prompt text,storage_key text,deleted_at timestamptz)`).Error; err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	if err := d.db.Exec(`INSERT INTO library_assets(id,user_id,name,asset_type,description,storage_key) VALUES (?,?,'หลิน','character','ศิษย์ฮวาซาน','characters/lin.png')`, id, d.user).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := modelark.NewRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	describer := &directorTestDescriber{}
	fetch := func(context.Context, string) ([]byte, string, error) {
		return []byte{0x89, 'P', 'N', 'G'}, "image/png", nil
	}
	d.facade = facade.NewDirectorFacade(d.store, repo.NewStudioRepository(d.db), d.media, d.analyzer, d.media, registry, nil, nil, facade.DirectorOptions{Enabled: true, AnalysisModel: "fixture-story", ImageModel: "fixture-image", StoryPoints: 4, ImagePoints: 3, VideoPoints: 20, MaxImageReferences: 1, LookDescriber: describer, ImageFetch: fetch})
	directorAdmissionApp(d, 1000)
	d.fund(100)
	var storyPrompt string
	d.analyzer.reply = func(prompt string) (string, error) {
		if strings.Contains(prompt, "DIRECTOR_STORY") {
			storyPrompt = prompt
		}
		out, err := directorPlanner(prompt)
		return strings.ReplaceAll(out, `"characterAssetIds":[]`, `"characterAssetIds":["`+id+`"]`), err
	}
	p := d.create()
	p = d.project("PUT", "/projects/"+p.ID+"/idea", map[string]any{"revision": p.Revision, "prompt": "หลินดวลกระบี่ที่ลานประลอง", "genre": "fantasy", "moods": []string{"ตื่นเต้น"}, "sceneCount": 1, "characterAssetIds": []string{id}, "storyModelId": "director-story"})
	p = d.action(p, "/cover")
	d.tick()
	p = d.action(d.get(p.ID), "/story")
	d.tick()
	p = d.action(d.get(p.ID), "/storyboard")
	for i := 0; i < 8 && p.Storyboard.Status != "succeeded"; i++ {
		d.tick()
		p = d.get(p.ID)
	}
	if p.Storyboard.Status != "succeeded" {
		t.Fatalf("storyboard: %+v", p.Storyboard)
	}
	if describer.calls != 1 {
		t.Fatalf("look must be described once per reference image, got %d calls", describer.calls)
	}
	if !strings.Contains(storyPrompt, "navy blue robe") {
		t.Fatal("planner must see the fixed character look")
	}
	if len(d.media.requests) < 3 {
		t.Fatalf("image requests: %d", len(d.media.requests))
	}
	for _, r := range d.media.requests {
		if !strings.Contains(r.Prompt, "navy blue robe with gold trim") || !strings.Contains(r.Prompt, "Never change a named character's") {
			t.Fatalf("every image prompt must carry the same look and the wardrobe lock (%d requests, refs %d):\n%q", len(d.media.requests), len(r.ReferenceURLs), r.Prompt)
		}
	}
}
