package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/gofiber/fiber/v2"
)

func directorBrowserPlanner(prompt string) (string, error) {
	if strings.Contains(prompt, "DIRECTOR_PLAN") {
		// Keep a real asynchronous planning window for browser edit/poll races.
		time.Sleep(2500 * time.Millisecond)
	}
	if !strings.Contains(prompt, "DIRECTOR_STORY") {
		return directorPlanner(prompt)
	}
	marker := "Project and instruction data: "
	start := strings.LastIndex(prompt, marker)
	if start < 0 {
		return "", errors.New("missing Director browser snapshot")
	}
	var in struct {
		Project dto.DirectorProject `json:"project"`
	}
	if err := json.Unmarshal([]byte(prompt[start+len(marker):]), &in); err != nil {
		return "", err
	}
	scenes := make([]map[string]any, len(in.Project.Scenes))
	for i := range scenes {
		scenes[i] = map[string]any{"title": fmt.Sprintf("การเดินทางฉากที่ %d", i+1), "summary": "เด็กน้อยออกเดินทางผ่านป่าและค้นพบดวงดาว", "mood": "อบอุ่น", "characterAssetIds": []string{}}
	}
	out, err := json.Marshal(map[string]any{"logline": "เด็กน้อยตามหาดาวจนค้นพบความกล้า", "scenes": scenes})
	return string(out), err
}

// Opt-in loopback fixture runs real HTTP handlers, persistence and worker state;
// only analysis/image/storage providers and session lookup are test doubles.
func TestDirectorBrowserFixture(t *testing.T) {
	if os.Getenv("DIRECTOR_BROWSER_FIXTURE") != "1" {
		t.Skip("opt-in browser fixture")
	}
	d := newDirectorFixture(t)
	shutdown := make(chan struct{})
	var shutdownOnce sync.Once
	d.fund(5000)
	d.analyzer.reply = directorBrowserPlanner
	d.app.Get("/open/v1/auth/me", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"data": fiber.Map{"id": d.user, "displayName": "Director fixture", "email": "fixture@example.invalid", "plan": "free", "role": "user"}})
	})
	d.app.Get("/open/v1/assets", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"data": fiber.Map{"items": []any{}, "total": 0, "page": 1, "limit": 50}})
	})
	d.app.Get("/__fixture/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"ready": true, "providers": "fake", "persistence": "isolated-postgres"})
	})
	d.app.Post("/__fixture/controls", func(c *fiber.Ctx) error {
		var in struct {
			Balance      *int `json:"balance"`
			InvalidImage bool `json:"invalidImage"`
			Stop         bool `json:"stop"`
		}
		if err := c.BodyParser(&in); err != nil || (in.Balance != nil && (*in.Balance < 0 || *in.Balance > 5000)) {
			return c.SendStatus(400)
		}
		if in.Balance != nil {
			if err := d.db.Exec("UPDATE director_point_accounts SET balance=? WHERE user_id=?", *in.Balance, d.user).Error; err != nil {
				return c.SendStatus(500)
			}
		}
		d.media.mu.Lock()
		d.media.invalid = in.InvalidImage
		d.media.mu.Unlock()
		if in.Stop {
			shutdownOnce.Do(func() { close(shutdown) })
		}
		return c.SendStatus(204)
	})
	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = d.facade.Tick(ctx)
			}
		}
	}()
	serverDone := make(chan error, 1)
	go func() { serverDone <- d.app.Listen("127.0.0.1:18139") }()
	t.Log("DIRECTOR_BROWSER_FIXTURE_READY loopback18139; real Go/isolated PG; fake analyzer/image/storage/auth; browser image interception required for owned.test and samples.test")
	select {
	case err := <-serverDone:
		t.Errorf("fixture server: %v", err)
	case <-shutdown:
	case <-time.After(45 * time.Minute):
	}
	cancel()
	<-workerDone
	if err := d.app.Shutdown(); err != nil {
		t.Error(err)
	}
}
