package repository_open_api

import (
	"context"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"gorm.io/gorm"
)

type ShareRepository interface {
	Create(ctx context.Context, share *models.VideoShare) error
	FindActiveByJob(ctx context.Context, jobID, userID string) (*models.VideoShare, error)
	// FindActiveByToken is the only lookup without a user filter: the token itself is
	// the credential for the public page.
	FindActiveByToken(ctx context.Context, token string) (*models.VideoShare, error)
	Revoke(ctx context.Context, id, userID string) error
	IncrementViews(ctx context.Context, id string) error
}

type shareRepository struct{ db *gorm.DB }

func NewShareRepository(db *gorm.DB) ShareRepository {
	return &shareRepository{db: db}
}

func (r *shareRepository) Create(ctx context.Context, share *models.VideoShare) error {
	if err := r.db.WithContext(ctx).Omit("Job").Create(share).Error; err != nil {
		return wrapErr("create share", err)
	}
	return nil
}

func (r *shareRepository) FindActiveByJob(ctx context.Context, jobID, userID string) (*models.VideoShare, error) {
	var s models.VideoShare
	result := r.db.WithContext(ctx).
		Where("job_id = ? AND user_id = ? AND revoked_at IS NULL", jobID, userID).
		First(&s)
	if result.Error != nil {
		return nil, wrapErr("find share by job", result.Error)
	}
	return &s, nil
}

func (r *shareRepository) FindActiveByToken(ctx context.Context, token string) (*models.VideoShare, error) {
	var s models.VideoShare
	result := r.db.WithContext(ctx).
		Joins("Job").
		Where("video_shares.token = ? AND video_shares.revoked_at IS NULL", token).
		Where(`"Job".deleted_at IS NULL AND "Job".status = ?`, models.JobStatusSucceeded).
		First(&s)
	if result.Error != nil {
		return nil, wrapErr("find share by token", result.Error)
	}
	return &s, nil
}

func (r *shareRepository) Revoke(ctx context.Context, id, userID string) error {
	result := r.db.WithContext(ctx).
		Model(&models.VideoShare{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", id, userID).
		Update("revoked_at", time.Now())
	if result.Error != nil {
		return wrapErr("revoke share", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *shareRepository) IncrementViews(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).
		Model(&models.VideoShare{}).
		Where("id = ?", id).
		UpdateColumn("view_count", gorm.Expr("view_count + 1"))
	if result.Error != nil {
		return wrapErr("increment share views", result.Error)
	}
	return nil
}
