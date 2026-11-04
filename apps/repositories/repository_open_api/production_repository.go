package repository_open_api

import (
	"context"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type ProductionRepository struct{ db *gorm.DB }

func NewProductionRepository(db *gorm.DB) *ProductionRepository { return &ProductionRepository{db} }
func (r *ProductionRepository) CreateWithScene(c context.Context, p *models.ProductionProject, s *models.Scene) error {
	return r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := tx.Create(p).Error; e != nil {
			return e
		}
		s.ProjectID = &p.ID
		return tx.Create(s).Error
	})
}
func (r *ProductionRepository) Find(c context.Context, id, user string) (*models.ProductionProject, error) {
	var p models.ProductionProject
	e := r.db.WithContext(c).Where("id=? AND user_id=?", id, user).First(&p).Error
	return &p, wrapErr("find project", e)
}
func (r *ProductionRepository) List(c context.Context, user string, page, limit int) ([]models.ProductionProject, int64, error) {
	out := []models.ProductionProject{}
	var n int64
	q := r.db.WithContext(c).Model(&models.ProductionProject{}).Where("user_id=?", user)
	if e := q.Count(&n).Error; e != nil {
		return nil, 0, e
	}
	e := q.Order("updated_at DESC").Limit(limit).Offset((page - 1) * limit).Find(&out).Error
	return out, n, e
}
func (r *ProductionRepository) Save(c context.Context, p *models.ProductionProject, revision int) error {
	res := r.db.WithContext(c).Model(&models.ProductionProject{}).Where("id=? AND user_id=? AND revision=?", p.ID, p.UserID, revision).Updates(map[string]any{"title": p.Title, "brief": p.Brief, "aspect_ratio": p.AspectRatio, "edit_draft": p.EditDraft, "revision": revision + 1, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrConflict
	}
	p.Revision = revision + 1
	return nil
}
func (r *ProductionRepository) Delete(c context.Context, id, user string) error {
	res := r.db.WithContext(c).Where("id=? AND user_id=?", id, user).Delete(&models.ProductionProject{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *ProductionRepository) Scenes(c context.Context, id, user string) ([]models.Scene, error) {
	out := []models.Scene{}
	e := r.db.WithContext(c).Where("project_id=? AND user_id=?", id, user).Order("scene_order ASC,created_at ASC").Find(&out).Error
	return out, e
}
func (r *ProductionRepository) AddScene(c context.Context, s *models.Scene) error {
	return r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		var p models.ProductionProject
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", s.ProjectID, s.UserID).First(&p).Error; e != nil {
			return wrapErr("project", e)
		}
		var n int64
		if e := tx.Model(&models.Scene{}).Where("project_id=?", p.ID).Count(&n).Error; e != nil {
			return e
		}
		if n >= 100 {
			return ErrConflict
		}
		var maxOrder int
		tx.Model(&models.Scene{}).Where("project_id=?", p.ID).Select("COALESCE(MAX(scene_order),-1)").Scan(&maxOrder)
		s.SceneOrder = maxOrder + 1
		if e := tx.Omit(clause.Associations).Create(s).Error; e != nil {
			return e
		}
		for _, t := range s.Tags {
			if e := tx.Create(&models.SceneTag{SceneID: s.ID, TagID: t.ID}).Error; e != nil {
				return e
			}
		}
		for _, ch := range s.Characters {
			if e := tx.Create(&models.SceneCharacter{SceneID: s.ID, CharacterProfileID: ch.CharacterProfileID, SortOrder: ch.SortOrder}).Error; e != nil {
				return e
			}
		}
		for _, asset := range s.Assets {
			asset.Base = models.Base{}
			asset.SceneID = s.ID
			if e := tx.Omit(clause.Associations).Create(&asset).Error; e != nil {
				return e
			}
		}
		return nil
	})
}
func (r *ProductionRepository) Reorder(c context.Context, id, user string, ids []string) error {
	return r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		var p models.ProductionProject
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", id, user).First(&p).Error; e != nil {
			return wrapErr("project", e)
		}
		var scenes []models.Scene
		if e := tx.Where("project_id=? AND user_id=?", id, user).Find(&scenes).Error; e != nil {
			return e
		}
		if len(ids) != len(scenes) {
			return ErrConflict
		}
		set := map[string]bool{}
		for _, s := range scenes {
			set[s.ID.String()] = true
		}
		for i, sid := range ids {
			if !set[sid] {
				return ErrConflict
			}
			delete(set, sid)
			if e := tx.Model(&models.Scene{}).Where("id=?", sid).Update("scene_order", i).Error; e != nil {
				return e
			}
		}
		return nil
	})
}
func (r *ProductionRepository) SceneJobs(c context.Context, sid, user string, page, limit int) ([]models.VideoGenerationJob, int64, error) {
	out := []models.VideoGenerationJob{}
	var n int64
	q := r.db.WithContext(c).Model(&models.VideoGenerationJob{}).Where("scene_id=? AND user_id=? AND mode<>?", sid, user, "text_image")
	if e := q.Count(&n).Error; e != nil {
		return nil, 0, e
	}
	e := q.Order("created_at DESC").Limit(limit).Offset((page - 1) * limit).Find(&out).Error
	return out, n, e
}
func (r *ProductionRepository) Exports(c context.Context, pid, user string) ([]models.StoryboardExport, error) {
	out := []models.StoryboardExport{}
	e := r.db.WithContext(c).Where("project_id=? AND user_id=?", pid, user).Order("created_at DESC").Limit(100).Find(&out).Error
	return out, e
}
func (r *ProductionRepository) UpdateSceneProduction(c context.Context, s *models.Scene) error {
	res := r.db.WithContext(c).Model(&models.Scene{}).Where("id=? AND user_id=?", s.ID, s.UserID).Updates(map[string]any{"reference_asset_id": s.ReferenceAssetID, "video_settings": s.VideoSettings})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
func (r *ProductionRepository) Asset(c context.Context, id, user string) (*models.LibraryAsset, error) {
	var a models.LibraryAsset
	e := r.db.WithContext(c).Where("id=? AND user_id=?", id, user).First(&a).Error
	return &a, wrapErr("asset", e)
}
func (r *ProductionRepository) ProjectForScene(c context.Context, s *models.Scene, user string) error {
	if s.ProjectID == nil {
		return nil
	}
	_, e := r.Find(c, s.ProjectID.String(), user)
	return e
}

// EnqueueVideo serializes a user's limits and idempotency without retrying paid calls.
func (r *ProductionRepository) EnqueueVideo(c context.Context, j *models.VideoGenerationJob, max int) (*models.VideoGenerationJob, bool, error) {
	var out *models.VideoGenerationJob
	created := false
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		var u models.User
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, "id=?", j.UserID).Error; e != nil {
			return e
		}
		if j.IdempotencyKey != nil {
			var existing models.VideoGenerationJob
			e := tx.Where("idempotency_key=? AND user_id=?", *j.IdempotencyKey, j.UserID).First(&existing).Error
			if e == nil {
				out = &existing
				return nil
			}
			if e != gorm.ErrRecordNotFound {
				return e
			}
		}
		var n int64
		if e := tx.Model(&models.VideoGenerationJob{}).Where("user_id=? AND status IN ?", j.UserID, []models.JobStatus{models.JobStatusQueued, models.JobStatusSubmitted, models.JobStatusProcessing}).Count(&n).Error; e != nil {
			return e
		}
		if n >= int64(max) {
			return ErrConflict
		}
		if e := tx.Create(j).Error; e != nil {
			return studioErr(e)
		}
		out = j
		created = true
		return nil
	})
	return out, created, e
}
