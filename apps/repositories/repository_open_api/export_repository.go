package repository_open_api

import (
	"context"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"gorm.io/gorm"
)

type ExportRepository interface {
	Create(ctx context.Context, export *models.StoryboardExport) error
	FindByID(ctx context.Context, id, userID string) (*models.StoryboardExport, error)
	List(ctx context.Context, userID string, page, limit int) ([]models.StoryboardExport, int64, error)
	UpdateStatus(ctx context.Context, id string, status models.ExportStatus, updates map[string]any) error
	// ListQueued returns exports ready to process, ordered oldest first.
	ListQueued(ctx context.Context, limit int) ([]models.StoryboardExport, error)
}

type exportRepository struct{ db *gorm.DB }

func NewExportRepository(db *gorm.DB) ExportRepository {
	return &exportRepository{db: db}
}

func (r *exportRepository) Create(ctx context.Context, export *models.StoryboardExport) error {
	if err := r.db.WithContext(ctx).Create(export).Error; err != nil {
		return wrapErr("create export", err)
	}
	return nil
}

func (r *exportRepository) FindByID(ctx context.Context, id, userID string) (*models.StoryboardExport, error) {
	var e models.StoryboardExport
	result := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", id, userID).
		First(&e)
	if result.Error != nil {
		return nil, wrapErr("find export", result.Error)
	}
	return &e, nil
}

func (r *exportRepository) List(ctx context.Context, userID string, page, limit int) ([]models.StoryboardExport, int64, error) {
	var exports []models.StoryboardExport
	var total int64

	base := r.db.WithContext(ctx).Model(&models.StoryboardExport{}).
		Where("user_id = ? AND deleted_at IS NULL", userID)

	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrapErr("count exports", err)
	}

	offset := (page - 1) * limit
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND deleted_at IS NULL", userID).
		Order("created_at DESC").
		Limit(limit).Offset(offset).
		Find(&exports)
	if result.Error != nil {
		return nil, 0, wrapErr("list exports", result.Error)
	}
	return exports, total, nil
}

func (r *exportRepository) UpdateStatus(ctx context.Context, id string, status models.ExportStatus, updates map[string]any) error {
	if updates == nil {
		updates = make(map[string]any)
	}
	updates["status"] = status
	updates["updated_at"] = time.Now()

	result := r.db.WithContext(ctx).
		Model(&models.StoryboardExport{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(updates)
	if result.Error != nil {
		return wrapErr("update export status", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *exportRepository) ListQueued(ctx context.Context, limit int) ([]models.StoryboardExport, error) {
	var exports []models.StoryboardExport
	result := r.db.WithContext(ctx).
		Where("status = ? AND deleted_at IS NULL", models.ExportStatusQueued).
		Order("created_at ASC").
		Limit(limit).
		Find(&exports)
	if result.Error != nil {
		return nil, wrapErr("list queued exports", result.Error)
	}
	return exports, nil
}
