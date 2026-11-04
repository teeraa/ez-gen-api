package repository_open_api

import (
	"context"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AIUsageRepository struct{ db *gorm.DB }

func NewAIUsageRepository(db *gorm.DB) *AIUsageRepository { return &AIUsageRepository{db: db} }

func (r *AIUsageRepository) Record(ctx context.Context, usage *models.AIUsage) error {
	if usage.Status == "started" {
		var count int64
		if err := r.db.WithContext(ctx).Model(&models.VideoGenerationJob{}).Where("id = ? AND user_id = ?", usage.JobID, usage.UserID).Count(&count).Error; err != nil {
			return wrapErr("verify AI usage owner", err)
		}
		if count != 1 {
			return ErrNotFound
		}
		return wrapErr("create AI usage", r.db.WithContext(ctx).Create(usage).Error)
	}
	query := r.db.WithContext(ctx).Model(&models.AIUsage{}).Where("user_id = ? AND job_id = ?", usage.UserID, usage.JobID)
	if usage.ID != uuid.Nil {
		query = query.Where("id = ?", usage.ID)
	} else if usage.ProviderTaskID != nil {
		query = query.Where("provider = ? AND provider_task_id = ?", usage.Provider, *usage.ProviderTaskID)
	} else {
		return ErrNotFound
	}
	// Terminal rows are immutable, so concurrent or repeated polls never add usage.
	openQuery := query.Session(&gorm.Session{}).Where("completed_at IS NULL")
	updates := map[string]any{"status": usage.Status}
	if usage.ProviderTaskID != nil {
		updates["provider_task_id"] = usage.ProviderTaskID
	}
	if usage.LatencyMS != nil {
		updates["latency_ms"] = usage.LatencyMS
	}
	if usage.CompletedAt != nil {
		updates["completed_at"] = usage.CompletedAt
	}
	if usage.InputTokens != nil {
		updates["input_tokens"] = usage.InputTokens
	}
	if usage.OutputTokens != nil {
		updates["output_tokens"] = usage.OutputTokens
	}
	if usage.TotalTokens != nil {
		updates["total_tokens"] = usage.TotalTokens
	}
	if usage.GeneratedImages != nil {
		updates["generated_images"] = usage.GeneratedImages
	}
	if usage.DurationSeconds != nil {
		updates["duration_seconds"] = usage.DurationSeconds
	}
	result := openQuery.Updates(updates)
	if result.Error != nil {
		return wrapErr("update AI usage", result.Error)
	}
	if result.RowsAffected > 0 {
		return nil
	}
	var existing int64
	if err := query.Count(&existing).Error; err != nil {
		return wrapErr("find AI usage", err)
	}
	if existing > 0 {
		return nil
	}
	if usage.ID != uuid.Nil || usage.ProviderTaskID == nil {
		return ErrNotFound
	}
	// Recover a submitted task when its post-submission update failed. A unique
	// open start is required; ambiguous retries must never be attributed by guess.
	var pending []models.AIUsage
	err := r.db.WithContext(ctx).Where("user_id = ? AND job_id = ? AND provider = ? AND operation = ? AND status = ? AND provider_task_id IS NULL AND completed_at IS NULL", usage.UserID, usage.JobID, usage.Provider, "video_generation", "started").Limit(2).Find(&pending).Error
	if err != nil {
		return wrapErr("recover AI usage", err)
	}
	if len(pending) == 0 {
		var historical int64
		if err := r.db.WithContext(ctx).Model(&models.AIUsage{}).Where("user_id = ? AND job_id = ?", usage.UserID, usage.JobID).Count(&historical).Error; err != nil {
			return wrapErr("find legacy AI usage", err)
		}
		// Jobs submitted before usage tracking have no attempt to update.
		if historical == 0 {
			return nil
		}
		return ErrNotFound
	}
	if len(pending) != 1 {
		return ErrNotFound
	}
	result = r.db.WithContext(ctx).Model(&models.AIUsage{}).Where("id = ? AND user_id = ? AND job_id = ? AND completed_at IS NULL", pending[0].ID, usage.UserID, usage.JobID).Updates(updates)
	if result.Error != nil {
		return wrapErr("recover AI usage", result.Error)
	}
	return nil
}
