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

type StoryFlowRepository struct {
	db        *gorm.DB
	maxActive int64
}

func NewStoryFlowRepository(db *gorm.DB, max int) *StoryFlowRepository {
	if max < 1 {
		max = 1
	}
	return &StoryFlowRepository{db, int64(max)}
}
func (r *StoryFlowRepository) Create(c context.Context, p *models.StoryFlowProject) (*models.StoryFlowProject, error) {
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", p.UserID.String()).Error; e != nil {
			return e
		}
		var old models.StoryFlowProject
		e := tx.Where("user_id=? AND idempotency_key=?", p.UserID, p.IdempotencyKey).First(&old).Error
		if e == nil {
			*p = old
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		return tx.Create(p).Error
	})
	return p, wrapErr("story create", e)
}
func (r *StoryFlowRepository) Get(c context.Context, id, user string) (*models.StoryFlowProject, error) {
	var p models.StoryFlowProject
	e := r.db.WithContext(c).Where("id=? AND user_id=?", id, user).First(&p).Error
	return &p, wrapErr("story get", e)
}
func (r *StoryFlowRepository) List(c context.Context, user string, page, limit int) ([]models.StoryFlowProject, int64, error) {
	q := r.db.WithContext(c).Model(&models.StoryFlowProject{}).Where("user_id=?", user)
	var n int64
	if e := q.Count(&n).Error; e != nil {
		return nil, 0, e
	}
	rows := []models.StoryFlowProject{}
	e := q.Order("created_at DESC,id DESC").Offset((page - 1) * limit).Limit(limit).Find(&rows).Error
	return rows, n, e
}
func (r *StoryFlowRepository) Change(c context.Context, id, user string, fn func(*models.StoryFlowProject) error) error {
	return wrapErr("story change", r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", user).Error; e != nil {
			return e
		}
		var p models.StoryFlowProject
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", id, user).First(&p).Error; e != nil {
			return e
		}
		wasActive := p.Status == "queued" || p.Status == "processing"
		if e := fn(&p); e != nil {
			return e
		}
		if !wasActive && (p.Status == "queued" || p.Status == "processing") {
			var n int64
			if e := tx.Model(&models.StoryFlowProject{}).Where("user_id=? AND status IN ?", user, []string{"queued", "processing"}).Count(&n).Error; e != nil {
				return e
			}
			if n >= r.maxActive {
				return ErrConflict
			}
		}
		return tx.Model(&p).Updates(map[string]any{"document": p.Document, "status": p.Status, "updated_at": time.Now()}).Error
	}))
}
func (r *StoryFlowRepository) Claim(c context.Context) (*models.StoryFlowProject, error) {
	var p models.StoryFlowProject
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status IN ? AND (lease_until IS NULL OR lease_until < ?)", []string{"queued", "processing"}, now).Order("updated_at,id").First(&p).Error; e != nil {
			return e
		}
		token := uuid.New()
		until := now.Add(20 * time.Minute)
		p.LeaseToken = &token
		p.LeaseUntil = &until
		return tx.Model(&p).Updates(map[string]any{"lease_token": token, "lease_until": until, "status": "processing"}).Error
	})
	return &p, wrapErr("story claim", e)
}
func (r *StoryFlowRepository) Save(c context.Context, p *models.StoryFlowProject, release bool) error {
	updates := map[string]any{"document": p.Document, "status": p.Status, "updated_at": time.Now()}
	if release {
		updates["lease_token"] = nil
		updates["lease_until"] = nil
	}
	q := r.db.WithContext(c).Model(&models.StoryFlowProject{}).Where("id=? AND user_id=? AND lease_token=?", p.ID, p.UserID, p.LeaseToken).Updates(updates)
	if q.Error != nil {
		return wrapErr("story save", q.Error)
	}
	if q.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

func (r *StoryFlowRepository) ByKey(c context.Context, key, user string) (*models.StoryFlowProject, error) {
	var p models.StoryFlowProject
	e := r.db.WithContext(c).Where("idempotency_key=? AND user_id=?", key, user).First(&p).Error
	return &p, wrapErr("story key", e)
}
