package repository_open_api

import (
	"context"
	"errors"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

type StudioRepository interface {
	CreateUser(context.Context, *models.User) error
	UserByEmail(context.Context, string) (*models.User, error)
	UserByID(context.Context, string) (*models.User, error)
	SaveAsset(context.Context, *models.LibraryAsset) error
	Asset(context.Context, string, string) (*models.LibraryAsset, error)
	Assets(context.Context, string, string, string, int, int) ([]models.LibraryAsset, int64, error)
	DeleteAsset(context.Context, string, string) error
	EnqueueImage(context.Context, *models.VideoGenerationJob, int64) (*models.VideoGenerationJob, error)
	ClaimImage(context.Context) (*models.VideoGenerationJob, error)
	PrepareImage(context.Context, string, string, []byte) error
	FinishImage(context.Context, string, models.JobStatus, map[string]any) error
}
type studioRepository struct{ db *gorm.DB }

func NewStudioRepository(db *gorm.DB) StudioRepository { return &studioRepository{db} }
func studioErr(e error) error {
	var p *pgconn.PgError
	if errors.As(e, &p) && p.Code == "23505" {
		return ErrConflict
	}
	return wrapErr("studio", e)
}
func (r *studioRepository) CreateUser(c context.Context, u *models.User) error {
	if e := r.db.WithContext(c).Create(u).Error; e != nil {
		return studioErr(e)
	}
	return nil
}
func (r *studioRepository) UserByEmail(c context.Context, email string) (*models.User, error) {
	var u models.User
	e := r.db.WithContext(c).Where("lower(email) = ?", strings.ToLower(email)).First(&u).Error
	if e != nil {
		return nil, studioErr(e)
	}
	return &u, nil
}
func (r *studioRepository) UserByID(c context.Context, id string) (*models.User, error) {
	var u models.User
	e := r.db.WithContext(c).Where("id = ?", id).First(&u).Error
	if e != nil {
		return nil, studioErr(e)
	}
	return &u, nil
}
func (r *studioRepository) SaveAsset(c context.Context, a *models.LibraryAsset) error {
	var e error
	if a.ID == uuid.Nil {
		e = r.db.WithContext(c).Create(a).Error
	} else {
		e = r.db.WithContext(c).Model(a).Where("user_id = ?", a.UserID).Select("name", "description", "updated_at").Updates(a).Error
	}
	if e != nil {
		return studioErr(e)
	}
	return nil
}
func (r *studioRepository) Asset(c context.Context, id, user string) (*models.LibraryAsset, error) {
	var a models.LibraryAsset
	e := r.db.WithContext(c).Where("id = ? AND user_id = ?", id, user).First(&a).Error
	if e != nil {
		return nil, studioErr(e)
	}
	return &a, nil
}
func (r *studioRepository) Assets(c context.Context, user, kind, search string, page, limit int) ([]models.LibraryAsset, int64, error) {
	q := r.db.WithContext(c).Model(&models.LibraryAsset{}).Where("user_id = ?", user)
	if kind != "" {
		q = q.Where("asset_type = ?", kind)
	}
	if search != "" {
		q = q.Where("name ILIKE ?", "%"+search+"%")
	}
	var n int64
	var a []models.LibraryAsset
	if e := q.Count(&n).Error; e != nil {
		return nil, 0, e
	}
	e := q.Order("created_at DESC").Limit(limit).Offset((page - 1) * limit).Find(&a).Error
	return a, n, e
}
func (r *studioRepository) DeleteAsset(c context.Context, id, user string) error {
	var references int64
	if e := r.db.WithContext(c).Model(&models.Scene{}).Where("reference_asset_id=?", id).Count(&references).Error; e != nil {
		return e
	}
	if references > 0 {
		return ErrConflict
	}
	res := r.db.WithContext(c).Where("id = ? AND user_id = ?", id, user).Delete(&models.LibraryAsset{})
	if res.Error != nil {
		return studioErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *studioRepository) EnqueueImage(c context.Context, j *models.VideoGenerationJob, max int64) (*models.VideoGenerationJob, error) {
	var out *models.VideoGenerationJob
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		// A per-user row lock serializes duplicate submissions and concurrent-job limits.
		var u models.User
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, "id = ?", j.UserID).Error; e != nil {
			return studioErr(e)
		}
		var old models.VideoGenerationJob
		e := tx.Where("idempotency_key = ? AND user_id = ?", *j.IdempotencyKey, j.UserID).First(&old).Error
		if e == nil {
			out = &old
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		var n int64
		if e := tx.Model(&models.VideoGenerationJob{}).Where("user_id = ? AND status IN ?", j.UserID, []string{"queued", "submitted", "processing"}).Count(&n).Error; e != nil {
			return e
		}
		if n >= max {
			return ErrConflict
		}
		if e := tx.Create(j).Error; e != nil {
			return studioErr(e)
		}
		out = j
		return nil
	})
	return out, e
}
func (r *studioRepository) ClaimImage(c context.Context) (*models.VideoGenerationJob, error) {
	var j models.VideoGenerationJob
	empty := false
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		// A lost worker never automatically resubmits a potentially billed request.
		cutoff := time.Now().Add(-10 * time.Minute)
		if e := tx.Model(&models.VideoGenerationJob{}).Where("mode = ? AND status = ? AND submitted_at < ?", "text_image", models.JobStatusProcessing, cutoff).Updates(map[string]any{"status": models.JobStatusFailed, "failure_code": "WORKER_INTERRUPTED", "failure_message": "การสร้างภาพถูกขัดจังหวะ กรุณาตรวจสอบยอดใช้งานก่อนลองใหม่", "completed_at": time.Now()}).Error; e != nil {
			return e
		}
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("mode = ? AND status = ?", "text_image", models.JobStatusQueued).Order("created_at ASC").First(&j).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				empty = true
				return nil
			}
			return studioErr(e)
		}
		now := time.Now()
		return tx.Model(&j).Updates(map[string]any{"status": models.JobStatusProcessing, "submitted_at": now, "current_step": "กำลังสร้างภาพ"}).Error
	})
	if e != nil {
		return nil, e
	}
	if empty {
		return nil, ErrNotFound
	}
	return &j, nil
}
func (r *studioRepository) FinishImage(c context.Context, id string, status models.JobStatus, fields map[string]any) error {
	fields["status"] = status
	fields["completed_at"] = time.Now()
	return r.db.WithContext(c).Model(&models.VideoGenerationJob{}).Where("id = ? AND status = ?", id, models.JobStatusProcessing).Updates(fields).Error
}

func (r *studioRepository) PrepareImage(c context.Context, id, prompt string, snapshot []byte) error {
	res := r.db.WithContext(c).Model(&models.VideoGenerationJob{}).Where("id = ? AND status = ?", id, models.JobStatusProcessing).Updates(map[string]any{"compiled_prompt": prompt, "effective_inputs": snapshot})
	if res.Error != nil {
		return studioErr(res.Error)
	}
	if res.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}
