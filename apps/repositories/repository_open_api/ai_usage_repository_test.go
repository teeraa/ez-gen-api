package repository_open_api

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAIUsageReferenceAnalysisMigration(t *testing.T) {
	dsn := os.Getenv("AI_USAGE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires explicitly isolated AI_USAGE_TEST_DATABASE_URL")
	}
	if !strings.Contains(dsn, "usage_test") {
		t.Fatal("requires isolated usage_test database/user")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	schema := "usage_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	for _, sql := range []string{"CREATE SCHEMA " + schema, "SET LOCAL search_path TO " + schema, "CREATE TABLE users (id uuid PRIMARY KEY)", "CREATE TABLE video_generation_jobs (id uuid PRIMARY KEY, user_id uuid NOT NULL, deleted_at timestamptz)"} {
		if err := tx.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	baseline, err := os.ReadFile("../../../migrations/017_ai_usage.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Keep the historical SQL inside this rollback-only fixture's transaction.
	sql := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(baseline)), "BEGIN;"), "COMMIT;")
	if err := tx.Exec(sql).Error; err != nil {
		t.Fatal(err)
	}
	var constraint int64
	if err := tx.Raw("SELECT count(*) FROM pg_constraint WHERE conrelid = 'ai_usage'::regclass AND conname = 'ai_usage_operation_check'").Scan(&constraint).Error; err != nil || constraint != 1 {
		t.Fatalf("constraint count=%d error=%v", constraint, err)
	}
	user, other, job := uuid.New(), uuid.New(), uuid.New()
	if err := tx.Exec("INSERT INTO users (id) VALUES (?), (?)", user, other).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("INSERT INTO video_generation_jobs (id,user_id) VALUES (?,?)", job, user).Error; err != nil {
		t.Fatal(err)
	}
	r := NewAIUsageRepository(tx)
	newAttempt := func(operation string) models.AIUsage {
		return models.AIUsage{ID: uuid.New(), UserID: user, JobID: job, Provider: "byteplus", Model: "fixture-model", Operation: operation, Status: "started", StartedAt: time.Now().UTC()}
	}
	legacy := newAttempt("video_generation")
	if err := r.Record(context.Background(), &legacy); err != nil {
		t.Fatal(err)
	}
	reject := func(attempt models.AIUsage) {
		t.Helper()
		if err := tx.Exec("SAVEPOINT rejected_usage").Error; err != nil {
			t.Fatal(err)
		}
		err := r.Record(context.Background(), &attempt)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23514" {
			t.Fatalf("expected check violation, got %v", err)
		}
		if err := tx.Exec("ROLLBACK TO SAVEPOINT rejected_usage").Error; err != nil {
			t.Fatal(err)
		}
	}
	reject(newAttempt("reference_analysis"))
	migration, err := os.ReadFile("../../../migrations/018_reference_analysis_usage.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("SAVEPOINT before_usage_migration").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"reference_analysis", "image_generation"} {
		attempt := newAttempt(operation)
		if err := r.Record(context.Background(), &attempt); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC()
		attempt.Status, attempt.CompletedAt = "succeeded", &now
		if err := r.Record(context.Background(), &attempt); err != nil {
			t.Fatal(err)
		}
	}
	wrong := newAttempt("reference_analysis")
	wrong.UserID = other
	if err := r.Record(context.Background(), &wrong); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong owner accepted: %v", err)
	}
	reject(newAttempt("unknown_operation"))
	var rows []models.AIUsage
	if err := tx.Where("job_id = ?", job).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("attempt count=%d", len(rows))
	}
	for _, row := range rows {
		if row.Operation == "video_generation" {
			if row.ID != legacy.ID || row.Status != "started" {
				t.Fatal("legacy row changed")
			}
		} else if row.Status != "succeeded" || row.CompletedAt == nil {
			t.Fatal("attempt completion lost")
		}
	}
	if err := tx.Exec("ROLLBACK TO SAVEPOINT before_usage_migration").Error; err != nil {
		t.Fatal(err)
	}
	reject(newAttempt("reference_analysis"))
	var remaining int64
	if err := tx.Model(&models.AIUsage{}).Count(&remaining).Error; err != nil || remaining != 1 {
		t.Fatalf("rollback retained unexpected rows count=%d error=%v", remaining, err)
	}
}

func TestAIUsagePersistence(t *testing.T) {
	dsn := os.Getenv("AI_USAGE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires explicitly isolated AI_USAGE_TEST_DATABASE_URL")
	}
	if !strings.Contains(dsn, "usage_test") {
		t.Fatal("requires isolated usage_test database/user")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	ctx := context.Background()
	user, other, job, legacy := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{user, other} {
		if err := tx.Exec("INSERT INTO users (id) VALUES (?)", id).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []uuid.UUID{job, legacy} {
		if err := tx.Exec("INSERT INTO video_generation_jobs (id,user_id) VALUES (?,?)", id, user).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := NewAIUsageRepository(tx)
	attempt := models.AIUsage{ID: uuid.New(), UserID: user, JobID: job, Provider: "byteplus", Model: "actual-model", Operation: "video_generation", Status: "started", StartedAt: time.Now().UTC()}
	wrong := attempt
	wrong.ID = uuid.New()
	wrong.UserID = other
	if err := repo.Record(ctx, &wrong); !errors.Is(err, ErrNotFound) {
		t.Fatalf("owner mismatch accepted %v", err)
	}
	if err := repo.Record(ctx, &attempt); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	zero, total := int64(0), int64(42)
	task := "fixture-task-" + uuid.NewString()
	// The post-submission write was lost; recover the unique durable start.
	result := models.AIUsage{UserID: user, JobID: job, Provider: "byteplus", ProviderTaskID: &task, Status: "succeeded", CompletedAt: &now, OutputTokens: &zero, TotalTokens: &total}
	if err := repo.Record(ctx, &result); err != nil {
		t.Fatal(err)
	}
	// Repeated terminal polls cannot overwrite or add to reported usage.
	total = 99
	if err := repo.Record(ctx, &result); err != nil {
		t.Fatal(err)
	}
	var rows []models.AIUsage
	if err := tx.Where("job_id = ?", job).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].InputTokens != nil || rows[0].OutputTokens == nil || *rows[0].OutputTokens != 0 || rows[0].TotalTokens == nil || *rows[0].TotalTokens != 42 || rows[0].Status != "succeeded" || rows[0].ProviderTaskID == nil || *rows[0].ProviderTaskID != task {
		t.Fatalf("persisted usage %+v", rows)
	}
	replay := attempt
	replay.Status = "submitted"
	replay.ProviderTaskID = &task
	if err := repo.Record(ctx, &replay); err != nil {
		t.Fatal(err)
	}
	var stored models.AIUsage
	if err := tx.First(&stored, "id = ?", attempt.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "succeeded" {
		t.Fatal("late submission regressed terminal status")
	}
	result.JobID = legacy
	if err := repo.Record(ctx, &result); err != nil {
		t.Fatalf("legacy poll blocked %v", err)
	}
	var count int64
	if err := tx.Model(&models.AIUsage{}).Where("job_id = ?", legacy).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("legacy poll invented usage count=%d error=%v", count, err)
	}
	// Multiple unresolved starts cannot be attributed by guessing.
	for i := 0; i < 2; i++ {
		pending := attempt
		pending.ID = uuid.New()
		if err := repo.Record(ctx, &pending); err != nil {
			t.Fatal(err)
		}
	}
	task = "other-task-" + uuid.NewString()
	result.JobID = job
	result.ProviderTaskID = &task
	if err := repo.Record(ctx, &result); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ambiguous attempt accepted %v", err)
	}
}
