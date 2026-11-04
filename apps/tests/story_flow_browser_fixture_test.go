package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This opt-in fixture exposes real handlers and persistence, with fake providers,
// solely for a separate production Next browser test. It never loads .env.
func TestStoryFlowBrowserFixture(t *testing.T) {
	if os.Getenv("STORY_FLOW_BROWSER_FIXTURE") != "1" {
		t.Skip("opt-in browser fixture")
	}
	dsn := os.Getenv("STORY_FLOW_TEST_DATABASE_URL")
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil || cfg.Host != "127.0.0.1" || cfg.Port != 15439 || cfg.Database != "story_flow_test" || cfg.User != "story_flow_test" || len(cfg.Fallbacks) > 0 {
		t.Fatal("refusing nonfixture database")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err = db.Exec("DROP TABLE IF EXISTS story_flow_projects; CREATE TABLE IF NOT EXISTS users (id UUID PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "020_story_flow.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	sf := sfNew(t, repo.NewStoryFlowRepository(db, 3))
	if err = db.Exec("INSERT INTO users(id) VALUES (?) ON CONFLICT DO NOTHING", sf.user).Error; err != nil {
		t.Fatal(err)
	}
	sf.video.failTask = "task2"
	sf.app.Get("/open/v1/auth/me", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"data": fiber.Map{"id": sf.user, "displayName": "Isolated fixture", "email": "fixture@example.invalid", "plan": "free"}})
	})
	sf.app.Get("/open/v1/assets", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"data": fiber.Map{"items": []string{}, "total": 0, "page": 1, "limit": 20}})
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				_ = sf.f.Tick(ctx)
			}
		}
	}()
	serverDone := make(chan error, 1)
	go func() { serverDone <- sf.app.Listen("127.0.0.1:18108") }()
	t.Log("STORY_FLOW_BROWSER_FIXTURE_READY loopback18108; real Go/PG; fake analysis/image/video/merge; browser media fixtures required")
	select {
	case err = <-serverDone:
		t.Errorf("fixture server: %v", err)
	case <-time.After(8 * time.Minute):
	}
	cancel()
	<-workerDone
	if err = sf.app.Shutdown(); err != nil {
		t.Error(err)
	}
}
