package repository_open_api

import (
	"context"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"gorm.io/gorm"
)

type JobRepository interface {
	Create(ctx context.Context, job *models.VideoGenerationJob) error
	FindByID(ctx context.Context, id, userID string) (*models.VideoGenerationJob, error)
	FindByProviderTaskID(ctx context.Context, providerTaskID string) (*models.VideoGenerationJob, error)
	FindByIdempotencyKey(ctx context.Context, key, userID string) (*models.VideoGenerationJob, error)
	List(ctx context.Context, userID string, page, limit int) ([]models.VideoGenerationJob, int64, error)
	CountActiveByUser(ctx context.Context, userID string) (int64, error)
	UpdateStatus(ctx context.Context, id string, status models.JobStatus, updates map[string]any) error
	UpdatePolled(ctx context.Context, id string, now time.Time) error
	ListPendingPoll(ctx context.Context, limit int) ([]models.VideoGenerationJob, error)
	ListUnpersistedOutputs(ctx context.Context, limit int) ([]models.VideoGenerationJob, error)
	UpdateOutputKeys(ctx context.Context, id string, mediaKey string, thumbKey *string) error
	UpdateCurrentStep(ctx context.Context, id string, step string) error
	AverageGenerationSeconds(ctx context.Context, modelID string, durationSeconds int, quality string, sample int) (float64, int, error)
}

type jobRepository struct{ db *gorm.DB }

func NewJobRepository(db *gorm.DB) JobRepository {
	return &jobRepository{db: db}
}

func (r *jobRepository) Create(ctx context.Context, job *models.VideoGenerationJob) error {
	if err := r.db.WithContext(ctx).Create(job).Error; err != nil {
		return wrapErr("create job", err)
	}
	return nil
}

func (r *jobRepository) FindByID(ctx context.Context, id, userID string) (*models.VideoGenerationJob, error) {
	var job models.VideoGenerationJob
	result := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", id, userID).
		First(&job)
	if result.Error != nil {
		return nil, wrapErr("find job", result.Error)
	}
	return &job, nil
}

func (r *jobRepository) FindByProviderTaskID(ctx context.Context, providerTaskID string) (*models.VideoGenerationJob, error) {
	var job models.VideoGenerationJob
	result := r.db.WithContext(ctx).
		Where("provider_task_id = ? AND deleted_at IS NULL", providerTaskID).
		First(&job)
	if result.Error != nil {
		return nil, wrapErr("find job by provider task id", result.Error)
	}
	return &job, nil
}

func (r *jobRepository) FindByIdempotencyKey(ctx context.Context, key, userID string) (*models.VideoGenerationJob, error) {
	var job models.VideoGenerationJob
	result := r.db.WithContext(ctx).
		Where("idempotency_key = ? AND user_id = ? AND deleted_at IS NULL", key, userID).
		First(&job)
	if result.Error != nil {
		return nil, wrapErr("find job by idempotency key", result.Error)
	}
	return &job, nil
}

func (r *jobRepository) List(ctx context.Context, userID string, page, limit int) ([]models.VideoGenerationJob, int64, error) {
	var jobs []models.VideoGenerationJob
	var total int64

	base := r.db.WithContext(ctx).Model(&models.VideoGenerationJob{}).
		Where("user_id = ? AND deleted_at IS NULL", userID)

	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrapErr("count jobs", err)
	}

	offset := (page - 1) * limit
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND deleted_at IS NULL", userID).
		Order("created_at DESC").
		Limit(limit).Offset(offset).
		Find(&jobs)
	if result.Error != nil {
		return nil, 0, wrapErr("list jobs", result.Error)
	}
	return jobs, total, nil
}

func (r *jobRepository) CountActiveByUser(ctx context.Context, userID string) (int64, error) {
	var count int64
	result := r.db.WithContext(ctx).Model(&models.VideoGenerationJob{}).
		Where("user_id = ? AND status IN ? AND deleted_at IS NULL",
			userID,
			[]models.JobStatus{models.JobStatusQueued, models.JobStatusSubmitted, models.JobStatusProcessing},
		).Count(&count)
	if result.Error != nil {
		return 0, wrapErr("count active jobs", result.Error)
	}
	return count, nil
}

func (r *jobRepository) UpdateStatus(ctx context.Context, id string, status models.JobStatus, updates map[string]any) error {
	if updates == nil {
		updates = make(map[string]any)
	}
	updates["status"] = status

	result := r.db.WithContext(ctx).
		Model(&models.VideoGenerationJob{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(updates)
	if result.Error != nil {
		return wrapErr("update job status", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *jobRepository) UpdatePolled(ctx context.Context, id string, now time.Time) error {
	result := r.db.WithContext(ctx).
		Model(&models.VideoGenerationJob{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("last_polled_at", now)
	if result.Error != nil {
		return wrapErr("update job polled at", result.Error)
	}
	return nil
}

func (r *jobRepository) ListPendingPoll(ctx context.Context, limit int) ([]models.VideoGenerationJob, error) {
	var jobs []models.VideoGenerationJob
	result := r.db.WithContext(ctx).
		Where("status IN ? AND deleted_at IS NULL",
			[]models.JobStatus{models.JobStatusSubmitted, models.JobStatusProcessing},
		).
		Order("last_polled_at ASC NULLS FIRST").
		Limit(limit).
		Find(&jobs)
	if result.Error != nil {
		return nil, wrapErr("list pending poll jobs", result.Error)
	}
	return jobs, nil
}

// ListUnpersistedOutputs finds succeeded jobs that still hold a provider URL
// instead of an object storage key.
func (r *jobRepository) ListUnpersistedOutputs(ctx context.Context, limit int) ([]models.VideoGenerationJob, error) {
	var jobs []models.VideoGenerationJob
	result := r.db.WithContext(ctx).
		Where("status = ? AND deleted_at IS NULL AND output_media_key LIKE ?", models.JobStatusSucceeded, "http%").
		Order("completed_at DESC").
		Limit(limit).
		Find(&jobs)
	if result.Error != nil {
		return nil, wrapErr("list unpersisted outputs", result.Error)
	}
	return jobs, nil
}

func (r *jobRepository) UpdateOutputKeys(ctx context.Context, id string, mediaKey string, thumbKey *string) error {
	result := r.db.WithContext(ctx).
		Model(&models.VideoGenerationJob{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{"output_media_key": mediaKey, "output_thumb_key": thumbKey})
	if result.Error != nil {
		return wrapErr("update output keys", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *jobRepository) UpdateCurrentStep(ctx context.Context, id string, step string) error {
	result := r.db.WithContext(ctx).
		Model(&models.VideoGenerationJob{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("current_step", step)
	if result.Error != nil {
		return wrapErr("update job current step", result.Error)
	}
	return nil
}

// AverageGenerationSeconds averages submit-to-complete time over the most recent
// succeeded jobs with the same duration and quality, across all users, since only
// the aggregate is returned. An empty modelID matches any model. Runs over 30
// minutes are excluded so a stuck job does not skew the estimate.
func (r *jobRepository) AverageGenerationSeconds(ctx context.Context, modelID string, durationSeconds int, quality string, sample int) (float64, int, error) {
	recent := r.db.WithContext(ctx).
		Model(&models.VideoGenerationJob{}).
		Select("EXTRACT(EPOCH FROM (completed_at - submitted_at)) AS secs").
		Where("status = ? AND deleted_at IS NULL", models.JobStatusSucceeded).
		Where("submitted_at IS NOT NULL AND completed_at IS NOT NULL").
		Where("completed_at - submitted_at BETWEEN interval '1 second' AND interval '30 minutes'").
		Where("duration_seconds = ? AND quality = ?", durationSeconds, quality)
	if modelID != "" {
		recent = recent.Where("model_id = ?", modelID)
	}
	recent = recent.Order("completed_at DESC").Limit(sample)

	var out struct {
		Avg *float64
		N   int
	}
	result := r.db.WithContext(ctx).
		Table("(?) AS recent", recent).
		Select("AVG(secs) AS avg, COUNT(*) AS n").
		Scan(&out)
	if result.Error != nil {
		return 0, 0, wrapErr("average generation seconds", result.Error)
	}
	if out.Avg == nil {
		return 0, 0, nil
	}
	return *out.Avg, out.N, nil
}
