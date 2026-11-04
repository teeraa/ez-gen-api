package repository_open_api

import (
	"bytes"
	"context"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPipelineOwnerAndLeaseCASQueries(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	db, e := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=fixture dbname=fixture sslmode=disable"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.New(log.New(&output, "", 0), logger.Config{LogLevel: logger.Info, Colorful: false})})
	if e != nil {
		t.Fatal(e)
	}
	r := NewPipelineRepository(db, 3)
	id, user, token := uuid.New(), uuid.New(), uuid.New()
	_, _ = r.Get(context.Background(), id.String(), user.String())
	sql := output.String()
	if !strings.Contains(sql, "user_id=") || !strings.Contains(sql, user.String()) || !strings.Contains(sql, `"deleted_at" IS NULL`) {
		t.Fatal(sql)
	}
	output.Reset()
	_ = r.Advance(context.Background(), &models.StudioPipeline{Base: models.Base{ID: id}, UserID: user, LeaseToken: &token, Stage: "video", Status: "processing"})
	sql = output.String()
	for _, term := range []string{"lease_token=", token.String(), "user_id=", user.String(), `"lease_token"=NULL`, `"lease_until"=NULL`} {
		if !strings.Contains(sql, term) {
			t.Fatalf("CAS missing %q: %s", term, sql)
		}
	}
	output.Reset()
	_ = db.Model(&models.StudioPipeline{}).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status IN ? AND (lease_until IS NULL OR lease_until < ?)", []string{"queued", "processing"}, time.Now()).First(&models.StudioPipeline{})
	if !strings.Contains(output.String(), "FOR UPDATE SKIP LOCKED") {
		t.Fatal(output.String())
	}
}
func TestPipelineMigrationSupportsVideoAndDurablePointers(t *testing.T) {
	t.Parallel()
	b, e := os.ReadFile("../../../migrations/019_studio_pipeline.sql")
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, term := range []string{"'video'", "UNIQUE(user_id,idempotency_key)", "video_attempt integer NOT NULL DEFAULT 0", "lease_token uuid", "lease_until timestamptz", "character_job_id uuid REFERENCES video_generation_jobs(id)", "video_asset_id uuid REFERENCES library_assets(id)", "'needs_attention'", "'save_character'", "'save_video'"} {
		if !strings.Contains(s, term) {
			t.Fatalf("missing schema invariant %q", term)
		}
	}
}
