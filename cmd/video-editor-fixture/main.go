package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/ez-gen/ez-gen-api/apps/crons"
	"github.com/ez-gen/ez-gen-api/apps/db"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	h "github.com/ez-gen/ez-gen-api/apps/handlers/handler_open_api"
	r "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/videoprocessing"
	"github.com/ez-gen/ez-gen-api/apps/utils"
	"github.com/ez-gen/ez-gen-api/middlewares"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const fixtureUser = "11111111-1111-4111-8111-111111111111"
const fixtureOther = "22222222-2222-4222-8222-222222222222"

type localStore struct{ root, base string }

func (s *localStore) path(key string) (string, error) {
	if key == "" || strings.Contains(key, "\\") || filepath.IsAbs(key) || filepath.Clean(key) != key || strings.HasPrefix(key, "..") {
		return "", fmt.Errorf("invalid fixture key")
	}
	return filepath.Join(s.root, key), nil
}
func (s *localStore) Upload(_ context.Context, key string, data []byte, kind string) error {
	path, e := s.path(key)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	if e = os.WriteFile(path, data, 0600); e != nil {
		return e
	}
	return os.WriteFile(path+".mime", []byte(kind), 0600)
}
func signature(key, expiry, name string) string {
	m := hmac.New(sha256.New, []byte("isolated-video-editor-fixture-signature"))
	m.Write([]byte(key + "\n" + expiry + "\n" + name))
	return hex.EncodeToString(m.Sum(nil))
}
func (s *localStore) sign(key string, ttl time.Duration, name string) (string, time.Time, error) {
	if _, e := s.path(key); e != nil {
		return "", time.Time{}, e
	}
	expiry := time.Now().Add(ttl)
	exp := strconv.FormatInt(expiry.Unix(), 10)
	q := url.Values{"key": {key}, "expires": {exp}, "filename": {name}, "signature": {signature(key, exp, name)}}
	return s.base + "/fixture/objects?" + q.Encode(), expiry, nil
}
func (s *localStore) SignedURL(_ context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	return s.sign(key, ttl, "")
}
func (s *localStore) SignedDownloadURL(_ context.Context, key string, ttl time.Duration, name string) (string, time.Time, error) {
	return s.sign(key, ttl, name)
}
func (s *localStore) serve(c *fiber.Ctx) error {
	key, exp, name := c.Query("key"), c.Query("expires"), c.Query("filename")
	actual, err := hex.DecodeString(c.Query("signature"))
	expected, _ := hex.DecodeString(signature(key, exp, name))
	expiry, e := strconv.ParseInt(exp, 10, 64)
	if err != nil || e != nil || expiry < time.Now().Unix() || !hmac.Equal(actual, expected) {
		return c.SendStatus(404)
	}
	path, e := s.path(key)
	if e != nil {
		return c.SendStatus(404)
	}
	kind, e := os.ReadFile(path + ".mime")
	if e != nil {
		return c.SendStatus(404)
	}
	c.Set("Content-Type", string(kind))
	c.Set("Cache-Control", "private, max-age=60")
	if name != "" {
		c.Attachment(filepath.Base(name))
	}
	return c.SendFile(path)
}
func main() {
	dsn := os.Getenv("VIDEO_EDITOR_FIXTURE_DSN")
	u, e := url.Parse(dsn)
	if e != nil || u.Host != "127.0.0.1:15457" || u.Path != "/ai_editor_phase1" {
		log.Fatal("fixture requires explicit isolated database at127.0.0.1:15457/ai_editor_phase1")
	}
	database, e := db.Connect(dsn, 10, 3)
	if e != nil {
		log.Fatal("fixture database unavailable")
	}
	if e = database.Exec(`CREATE TABLE IF NOT EXISTS users(id UUID PRIMARY KEY,created_at timestamptz DEFAULT now(),updated_at timestamptz DEFAULT now(),deleted_at timestamptz,email text UNIQUE,display_name text,role text DEFAULT 'user',plan text DEFAULT 'free',password_hash text);`).Error; e != nil {
		log.Fatal(e)
	}
	if !database.Migrator().HasTable("video_editor_projects") {
		migration, e := os.ReadFile("migrations/021_video_editor.sql")
		if e != nil {
			log.Fatal(e)
		}
		if e = database.Exec(string(migration)).Error; e != nil {
			log.Fatal(e)
		}
	}
	if !database.Migrator().HasColumn("video_editor_assets", "kind") {
		migration, e := os.ReadFile("migrations/024_video_editor_audio_text.sql")
		if e != nil {
			log.Fatal(e)
		}
		if e = database.Exec(string(migration)).Error; e != nil {
			log.Fatal(e)
		}
	}
	if e = database.Exec(`INSERT INTO users(id,email,display_name) VALUES (?, 'editor@example.invalid','Editor Fixture'),(?, 'other@example.invalid','Other Fixture') ON CONFLICT(id) DO NOTHING`, fixtureUser, fixtureOther).Error; e != nil {
		log.Fatal(e)
	}
	root := os.Getenv("VIDEO_EDITOR_FIXTURE_STORAGE")
	if root == "" {
		root = "/private/tmp/ai-editor-phase1/storage"
	}
	store := &localStore{root: root, base: "http://127.0.0.1:18157"}
	facade := f.NewVideoEditorFacade(r.NewVideoEditorRepository(database), store, videoprocessing.NewFFmpegProcessor(os.Getenv("FFMPEG_FONT_FILE")))
	app := fiber.New(fiber.Config{DisablePreParseMultipartForm: true, BodyLimit: 55 << 20, ReadTimeout: 60 * time.Second, WriteTimeout: 150 * time.Second, ErrorHandler: func(c *fiber.Ctx, e error) error {
		if e == fiber.ErrRequestEntityTooLarge {
			return utils.Error(c, 413, "FILE_TOO_LARGE", "ไฟล์ใหญ่เกินกำหนด", nil)
		}
		return utils.Error(c, 500, "INTERNAL_ERROR", "Fixture request failed", nil)
	}})
	app.Use(middlewares.RequestID())
	app.Use(recover.New())
	app.Get("/health", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok"}) })
	app.Get("/fixture/objects", store.serve)
	group := app.Group("/open/v1", func(c *fiber.Ctx) error {
		token := strings.TrimPrefix(c.Get("Authorization"), "Bearer ")
		user := fixtureUser
		if token == "video-editor-other" {
			user = fixtureOther
		} else if token != "video-editor-fixture" {
			return utils.Error(c, 401, "UNAUTHORIZED", "Fixture authentication required", nil)
		}
		c.Locals("userID", user)
		return c.Next()
	})
	group.Get("/auth/me", func(c *fiber.Ctx) error {
		return utils.Success(c, 200, struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			Email       string `json:"email"`
			Plan        string `json:"plan"`
			Role        string `json:"role"`
		}{c.Locals("userID").(string), "Editor Fixture", "editor@example.invalid", "free", "user"})
	})
	h.NewVideoEditorHandler(facade).RegisterRoutes(group)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go crons.NewVideoEditorWorker(facade, 250*time.Millisecond).Start(ctx)
	go func() { <-ctx.Done(); _ = app.ShutdownWithTimeout(5 * time.Second) }()
	log.Println("isolated video editor fixture listening on127.0.0.1:18157")
	if e = app.Listen("127.0.0.1:18157"); e != nil {
		log.Fatal(e)
	}
}
