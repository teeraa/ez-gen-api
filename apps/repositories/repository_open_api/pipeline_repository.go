package repository_open_api

import (
	"context"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type PipelineRepository struct {
	db        *gorm.DB
	maxActive int64
}

func NewPipelineRepository(db *gorm.DB, max int) *PipelineRepository {
	if max < 1 {
		max = 3
	}
	return &PipelineRepository{db: db, maxActive: int64(max)}
}
func (r *PipelineRepository) Create(c context.Context, p *models.StudioPipeline) (*models.StudioPipeline, error) {
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", p.UserID.String()).Error; err != nil {
			return err
		}
		var existing models.StudioPipeline
		err := tx.Where("user_id=? AND idempotency_key=?", p.UserID, p.IdempotencyKey).First(&existing).Error
		if err == nil {
			return nil
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		var n int64
		if err = tx.Model(&models.StudioPipeline{}).Where("user_id=? AND status IN ?", p.UserID, []string{"queued", "processing"}).Count(&n).Error; err != nil {
			return err
		}
		if n >= r.maxActive {
			return ErrConflict
		}
		return tx.Create(p).Error
	})
	if e != nil {
		return nil, studioErr(e)
	}
	var out models.StudioPipeline
	e = r.db.WithContext(c).Where("user_id=? AND idempotency_key=?", p.UserID, p.IdempotencyKey).First(&out).Error
	return &out, studioErr(e)
}
func (r *PipelineRepository) ByKey(c context.Context, key, user string) (*models.StudioPipeline, error) {
	var p models.StudioPipeline
	e := r.db.WithContext(c).Where("idempotency_key=? AND user_id=?", key, user).First(&p).Error
	return &p, studioErr(e)
}
func (r *PipelineRepository) Get(c context.Context, id, user string) (*models.StudioPipeline, error) {
	var p models.StudioPipeline
	e := r.db.WithContext(c).Where("id=? AND user_id=?", id, user).First(&p).Error
	return &p, studioErr(e)
}
func (r *PipelineRepository) List(c context.Context, user string, page, limit int) ([]models.StudioPipeline, int64, error) {
	q := r.db.WithContext(c).Model(&models.StudioPipeline{}).Where("user_id=?", user)
	var n int64
	if e := q.Count(&n).Error; e != nil {
		return nil, 0, e
	}
	var rows []models.StudioPipeline
	e := q.Order("created_at DESC, id DESC").Offset((page - 1) * limit).Limit(limit).Find(&rows).Error
	return rows, n, e
}
func (r *PipelineRepository) Claim(c context.Context) (*models.StudioPipeline, error) {
	var p models.StudioPipeline
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status IN ? AND (lease_until IS NULL OR lease_until < ?)", []string{"queued", "processing"}, now).Order("updated_at, id").First(&p).Error; e != nil {
			return e
		}
		token := uuid.New()
		until := now.Add(10 * time.Minute)
		p.LeaseToken = &token
		p.LeaseUntil = &until
		p.Status = "processing"
		return tx.Model(&p).Updates(map[string]any{"lease_token": token, "lease_until": until, "status": "processing"}).Error
	})
	return &p, studioErr(e)
}
func (r *PipelineRepository) Advance(c context.Context, p *models.StudioPipeline) error {
	q := r.db.WithContext(c).Model(&models.StudioPipeline{}).Where("id=? AND user_id=? AND lease_token=?", p.ID, p.UserID, p.LeaseToken).Updates(map[string]any{"status": p.Status, "stage": p.Stage, "character_job_id": p.CharacterJobID, "character_asset_id": p.CharacterAssetID, "video_job_id": p.VideoJobID, "video_asset_id": p.VideoAssetID, "failure_code": p.FailureCode, "failure_message": p.FailureMessage, "lease_token": nil, "lease_until": nil, "updated_at": time.Now()})
	if q.Error != nil {
		return studioErr(q.Error)
	}
	if q.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}
func (r *PipelineRepository) Retry(c context.Context, id, user string) error {
	return r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", user).Error; e != nil {
			return e
		}
		var p models.StudioPipeline
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", id, user).First(&p).Error; e != nil {
			return studioErr(e)
		}
		if p.Status == "queued" || p.Status == "processing" {
			return nil
		}
		if p.Status != "failed" {
			return ErrConflict
		}
		var active int64
		if e := tx.Model(&models.StudioPipeline{}).Where("user_id=? AND status IN ?", user, []string{"queued", "processing"}).Count(&active).Error; e != nil {
			return e
		}
		if active >= r.maxActive {
			return ErrConflict
		}

		updates := map[string]any{"status": "queued", "failure_code": nil, "failure_message": nil, "lease_token": nil, "lease_until": nil, "updated_at": time.Now()}
		if p.Stage == "video" {
			if p.VideoJobID == nil {
				return ErrConflict
			}
			var j models.VideoGenerationJob
			if e := tx.Where("id=? AND user_id=?", p.VideoJobID, user).First(&j).Error; e != nil {
				return studioErr(e)
			}
			if (j.Status != models.JobStatusFailed && j.Status != models.JobStatusCancelled) || (j.FailureCode != nil && (*j.FailureCode == "SUBMISSION_INTERRUPTED" || *j.FailureCode == "WORKER_INTERRUPTED" || *j.FailureCode == "PROVIDER_TIMEOUT" || *j.FailureCode == "PROVIDER_ERROR")) {
				return ErrConflict
			}
			updates["video_job_id"] = nil
			updates["video_attempt"] = p.VideoAttempt + 1
		} else if p.Stage != "save_character" && p.Stage != "save_video" {
			return ErrConflict
		}
		return tx.Model(&p).Updates(updates).Error
	})
}
func (r *PipelineRepository) InsertAsset(c context.Context, a *models.LibraryAsset) error {
	if e := r.db.WithContext(c).Clauses(clause.OnConflict{DoNothing: true}).Create(a).Error; e != nil {
		return studioErr(e)
	}
	var old models.LibraryAsset
	if e := r.db.WithContext(c).Where("job_id=? AND user_id=?", a.JobID, a.UserID).First(&old).Error; e != nil {
		return studioErr(e)
	}
	if old.JobID == nil || a.JobID == nil || *old.JobID != *a.JobID || old.StorageKey == nil || a.StorageKey == nil || *old.StorageKey != *a.StorageKey || old.AssetType != a.AssetType || old.MediaType == nil || a.MediaType == nil || *old.MediaType != *a.MediaType {
		return ErrConflict
	}
	a.ID = old.ID
	return nil
}
