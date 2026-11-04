package repository_open_api

import (
	"context"
	"errors"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

var (
	ErrVERevision       = errors.New("video editor revision conflict")
	ErrVEIdempotency    = errors.New("video editor idempotency conflict")
	ErrVEUploadProgress = errors.New("video editor upload in progress")
	ErrVEUploadLimit    = errors.New("video editor asset limit")
	ErrVEUploadBusy     = errors.New("video editor upload busy")
	ErrVEExportState    = errors.New("video editor export state")
)

type VideoEditorRepository struct{ db *gorm.DB }

func NewVideoEditorRepository(db *gorm.DB) *VideoEditorRepository {
	return &VideoEditorRepository{db: db}
}
func veError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
func (r *VideoEditorRepository) CreateProject(ctx context.Context, p *models.VideoEditorProject) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "request_id"}}, DoNothing: true}).Create(p)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 1 {
		return true, nil
	}
	var old models.VideoEditorProject
	if e := r.db.WithContext(ctx).Where("user_id=? AND request_id=?", p.UserID, p.RequestID).First(&old).Error; e != nil {
		return false, veError(e)
	}
	if old.RequestHash != p.RequestHash {
		return false, ErrVEIdempotency
	}
	*p = old
	return false, nil
}
func (r *VideoEditorRepository) Project(ctx context.Context, id, user string) (*models.VideoEditorProject, error) {
	var p models.VideoEditorProject
	e := r.db.WithContext(ctx).Where("id=? AND user_id=?", id, user).First(&p).Error
	return &p, veError(e)
}
func (r *VideoEditorRepository) Projects(ctx context.Context, user string, page, limit int) ([]models.VideoEditorProject, int64, error) {
	out := []models.VideoEditorProject{}
	var n int64
	q := r.db.WithContext(ctx).Model(&models.VideoEditorProject{}).Where("user_id=?", user)
	if e := q.Count(&n).Error; e != nil {
		return nil, 0, e
	}
	e := q.Order("updated_at DESC,id DESC").Limit(limit).Offset((page - 1) * limit).Find(&out).Error
	return out, n, e
}
func (r *VideoEditorRepository) Assets(ctx context.Context, id, user string) ([]models.VideoEditorAsset, error) {
	out := []models.VideoEditorAsset{}
	e := r.db.WithContext(ctx).Where("project_id=? AND user_id=?", id, user).Order("created_at,id").Find(&out).Error
	return out, e
}
func (r *VideoEditorRepository) Save(ctx context.Context, p *models.VideoEditorProject, revision int) error {
	values := map[string]any{"title": p.Title, "aspect_ratio": p.AspectRatio, "segments": p.Segments, "audio_clips": p.AudioClips, "text_layers": p.TextLayers, "revision": revision + 1, "updated_at": time.Now().UTC()}
	// image_layers exists only after migration 028; the facade sets it only then.
	if p.ImageLayers != nil {
		values["image_layers"] = p.ImageLayers
	}
	res := r.db.WithContext(ctx).Model(&models.VideoEditorProject{}).Where("id=? AND user_id=? AND revision=?", p.ID, p.UserID, revision).Updates(values)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrVERevision
	}
	p.Revision = revision + 1
	p.UpdatedAt = time.Now().UTC()
	return nil
}
func (r *VideoEditorRepository) ReserveAsset(ctx context.Context, a *models.VideoEditorAsset) (bool, error) {
	created := false
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var u models.User
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id=?", a.UserID).First(&u).Error; e != nil {
			return veError(e)
		}
		var p models.VideoEditorProject
		if e := tx.Where("id=? AND user_id=?", a.ProjectID, a.UserID).First(&p).Error; e != nil {
			return veError(e)
		}
		var old models.VideoEditorAsset
		e := tx.Where("project_id=? AND user_id=? AND client_upload_id=?", a.ProjectID, a.UserID, a.ClientUploadID).First(&old).Error
		if e == nil {
			if old.ContentHash != a.ContentHash {
				return ErrVEIdempotency
			}
			if old.Status == "ready" {
				*a = old
				return nil
			}
			if old.Status == "processing" && old.LeaseUntil != nil && old.LeaseUntil.After(time.Now()) {
				return ErrVEUploadProgress
			}
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		var active int64
		if e := tx.Model(&models.VideoEditorAsset{}).Where("user_id=? AND status='processing' AND lease_until>?", a.UserID, time.Now()).Count(&active).Error; e != nil {
			return e
		}
		if active >= 2 {
			return ErrVEUploadBusy
		}
		if old.ID != uuid.Nil {
			res := tx.Model(&old).Updates(map[string]any{"status": "processing", "attempt_token": a.AttemptToken, "lease_until": a.LeaseUntil, "failure_code": "", "failure_message": "", "updated_at": time.Now()})
			if res.Error != nil {
				return res.Error
			}
			old.Status = "processing"
			old.AttemptToken = a.AttemptToken
			old.LeaseUntil = a.LeaseUntil
			old.FailureCode = ""
			old.FailureMessage = ""
			*a = old
			created = true
			return nil
		}
		var n int64
		if e := tx.Model(&models.VideoEditorAsset{}).Where("project_id=? AND user_id=?", a.ProjectID, a.UserID).Count(&n).Error; e != nil {
			return e
		}
		if n >= 100 {
			return ErrVEUploadLimit
		}
		if e := tx.Create(a).Error; e != nil {
			return e
		}
		created = true
		return nil
	})
	return created, e
}
func (r *VideoEditorRepository) CompleteAsset(ctx context.Context, a *models.VideoEditorAsset) error {
	if a.Kind == "" {
		a.Kind = "video"
	}
	res := r.db.WithContext(ctx).Model(&models.VideoEditorAsset{}).Where("id=? AND user_id=? AND attempt_token=? AND status='processing'", a.ID, a.UserID, a.AttemptToken).Updates(map[string]any{"status": "ready", "kind": a.Kind, "duration": a.Duration, "width": a.Width, "height": a.Height, "has_audio": a.HasAudio, "source_key": a.SourceKey, "frames": a.Frames, "lease_until": nil, "updated_at": time.Now(), "failure_code": "", "failure_message": ""})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrVEUploadProgress
	}
	a.Status = "ready"
	return nil
}
func (r *VideoEditorRepository) FailAsset(ctx context.Context, id, user string, token uuid.UUID, code, message string) error {
	return r.db.WithContext(ctx).Model(&models.VideoEditorAsset{}).Where("id=? AND user_id=? AND attempt_token=? AND status='processing'", id, user, token).Updates(map[string]any{"status": "failed", "failure_code": code, "failure_message": message, "lease_until": nil, "updated_at": time.Now()}).Error
}
func (r *VideoEditorRepository) ExistingExport(ctx context.Context, user, request string) (*models.VideoEditorExport, error) {
	var e models.VideoEditorExport
	err := r.db.WithContext(ctx).Where("user_id=? AND request_id=?", user, request).First(&e).Error
	return &e, veError(err)
}
func (r *VideoEditorRepository) CreateExport(ctx context.Context, e *models.VideoEditorExport) (bool, error) {
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p models.VideoEditorProject
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", e.ProjectID, e.UserID).First(&p).Error; err != nil {
			return veError(err)
		}
		var old models.VideoEditorExport
		if err := tx.Where("user_id=? AND request_id=?", e.UserID, e.RequestID).First(&old).Error; err == nil {
			if old.RequestHash != e.RequestHash {
				return ErrVEIdempotency
			}
			*e = old
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if p.Revision != e.Revision {
			return ErrVERevision
		}
		res := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "request_id"}}, DoNothing: true}).Create(e)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 1 {
			created = true
			return nil
		}
		if err := tx.Where("user_id=? AND request_id=?", e.UserID, e.RequestID).First(&old).Error; err != nil {
			return err
		}
		if old.RequestHash != e.RequestHash {
			return ErrVEIdempotency
		}
		*e = old
		return nil
	})
	return created, err
}
func (r *VideoEditorRepository) ExpireExports(ctx context.Context) error {
	return r.db.WithContext(ctx).Model(&models.VideoEditorExport{}).Where("status='processing' AND lease_until<?", time.Now()).Updates(map[string]any{"status": "failed", "failure_code": "RENDER_INTERRUPTED", "failure_message": "การส่งออกหยุดชะงัก กรุณาลองใหม่", "lease_until": nil, "updated_at": time.Now()}).Error
}
func (r *VideoEditorRepository) Exports(ctx context.Context, id, user string) ([]models.VideoEditorExport, error) {
	out := []models.VideoEditorExport{}
	e := r.db.WithContext(ctx).Where("project_id=? AND user_id=?", id, user).Order("created_at DESC,id DESC").Limit(100).Find(&out).Error
	return out, e
}
func (r *VideoEditorRepository) Export(ctx context.Context, id, user string) (*models.VideoEditorExport, error) {
	var e models.VideoEditorExport
	err := r.db.WithContext(ctx).Where("id=? AND user_id=?", id, user).First(&e).Error
	return &e, veError(err)
}
func (r *VideoEditorRepository) Retry(ctx context.Context, id, user string) error {
	res := r.db.WithContext(ctx).Model(&models.VideoEditorExport{}).Where("id=? AND user_id=? AND status='failed' AND attempts<3", id, user).Updates(map[string]any{"status": "queued", "failure_code": "", "failure_message": "", "lease_until": nil, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 1 {
		return nil
	}
	e, err := r.Export(ctx, id, user)
	if err != nil {
		return err
	}
	if e.Status == "queued" || e.Status == "processing" || e.Status == "completed" {
		return nil
	}
	return ErrVEExportState
}
func (r *VideoEditorRepository) Claim(ctx context.Context) (*models.VideoEditorExport, error) {
	var e models.VideoEditorExport
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status='queued'").Order("created_at,id").First(&e).Error; err != nil {
			return veError(err)
		}
		token := uuid.New()
		lease := time.Now().Add(25 * time.Minute)
		if err := tx.Model(&e).Updates(map[string]any{"status": "processing", "attempt_token": token, "lease_until": lease, "attempts": e.Attempts + 1, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		e.Status = "processing"
		e.AttemptToken = &token
		e.LeaseUntil = &lease
		e.Attempts++
		return nil
	})
	return &e, err
}
func (r *VideoEditorRepository) FinishExport(ctx context.Context, e *models.VideoEditorExport, key, code, message string) error {
	status := "completed"
	if code != "" {
		status = "failed"
	}
	res := r.db.WithContext(ctx).Model(&models.VideoEditorExport{}).Where("id=? AND user_id=? AND status='processing' AND attempt_token=?", e.ID, e.UserID, e.AttemptToken).Updates(map[string]any{"status": status, "output_key": key, "failure_code": code, "failure_message": message, "lease_until": nil, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrVEExportState
	}
	return nil
}

// AudioTextReady reports whether migration 024's columns exist.
// ImageLayersReady reports whether migration 028 (image_layers column) is applied.
func (r *VideoEditorRepository) ImageLayersReady(ctx context.Context) bool {
	return r.db.WithContext(ctx).Migrator().HasColumn(&models.VideoEditorProject{}, "image_layers")
}
func (r *VideoEditorRepository) AudioTextReady(ctx context.Context) bool {
	m := r.db.WithContext(ctx).Migrator()
	return m.HasColumn(&models.VideoEditorAsset{}, "kind") && m.HasColumn(&models.VideoEditorProject{}, "audio_clips") && m.HasColumn(&models.VideoEditorProject{}, "text_layers")
}
