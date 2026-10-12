package repository_open_api

import (
	"context"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"gorm.io/gorm"
)

type UploadRepository interface {
	Create(ctx context.Context, upload *models.Upload) error
	FindByID(ctx context.Context, id, userID string) (*models.Upload, error)
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}

type uploadRepository struct{ db *gorm.DB }

func NewUploadRepository(db *gorm.DB) UploadRepository {
	return &uploadRepository{db: db}
}

func (r *uploadRepository) Create(ctx context.Context, upload *models.Upload) error {
	if err := r.db.WithContext(ctx).Create(upload).Error; err != nil {
		return wrapErr("create upload", err)
	}
	return nil
}

func (r *uploadRepository) FindByID(ctx context.Context, id, userID string) (*models.Upload, error) {
	var u models.Upload
	result := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", id, userID).
		First(&u)
	if result.Error != nil {
		return nil, wrapErr("find upload", result.Error)
	}
	return &u, nil
}

func (r *uploadRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("expires_at < ? AND deleted_at IS NULL", before).
		Delete(&models.Upload{})
	if result.Error != nil {
		return 0, wrapErr("delete expired uploads", result.Error)
	}
	return result.RowsAffected, nil
}
