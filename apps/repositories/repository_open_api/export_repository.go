package repository_open_api

import (
	"context"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ExportRepository interface {
	FindByIdempotencyKey(context.Context, string, string) (*models.StoryboardExport, error)
	Retry(context.Context, string, string) error
	Finish(context.Context, string, *time.Time, models.ExportStatus, map[string]any) error
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
		return studioErr(err)
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
	var out []models.StoryboardExport
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Recover only render work: a failed export requires an explicit user retry.
		if e := tx.Model(&models.StoryboardExport{}).Where("status=? AND last_picked_at < ?", models.ExportStatusProcessing, time.Now().Add(-30*time.Minute)).Updates(map[string]any{"status": models.ExportStatusFailed, "failure_message": "การส่งออกหยุดทำงาน โปรดลองอีกครั้ง", "completed_at": time.Now()}).Error; e != nil {
			return e
		}
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status=?", models.ExportStatusQueued).Order("created_at ASC").Limit(limit).Find(&out).Error; e != nil {
			return e
		}
		now := time.Now()
		for i := range out {
			res := tx.Model(&models.StoryboardExport{}).Where("id=? AND status=?", out[i].ID, models.ExportStatusQueued).Updates(map[string]any{"status": models.ExportStatusProcessing, "last_picked_at": now, "started_at": now})
			if res.Error != nil {
				return res.Error
			}
			out[i].Status = models.ExportStatusProcessing
			out[i].LastPickedAt = &now
		}
		return nil
	})
	return out, e
}
func (r *exportRepository) FindByIdempotencyKey(c context.Context, key, user string) (*models.StoryboardExport, error) {
	var e models.StoryboardExport
	err := r.db.WithContext(c).Where("user_id=? AND idempotency_key=?", user, key).First(&e).Error
	if err != nil {
		return nil, wrapErr("find export", err)
	}
	return &e, nil
}
func (r *exportRepository) Retry(c context.Context, id, user string) error {
	res := r.db.WithContext(c).Model(&models.StoryboardExport{}).Where("id=? AND user_id=? AND status=?", id, user, models.ExportStatusFailed).Updates(map[string]any{"status": models.ExportStatusQueued, "failure_message": nil, "completed_at": nil, "started_at": nil, "last_picked_at": nil})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

func (r *exportRepository) Finish(c context.Context, id string, lease *time.Time, status models.ExportStatus, updates map[string]any) error {
	updates["status"] = status
	updates["updated_at"] = time.Now()
	q := r.db.WithContext(c).Model(&models.StoryboardExport{}).Where("id=? AND status=?", id, models.ExportStatusProcessing)
	if lease != nil {
		q = q.Where("last_picked_at=?", lease)
	} else {
		q = q.Where("last_picked_at IS NULL")
	}
	res := q.Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}
