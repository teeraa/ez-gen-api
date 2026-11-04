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

var ErrDirectorIdempotency = errors.New("director idempotency conflict")

type DirectorInsufficient struct {
	Required int
	Balance  int64
}

func (e *DirectorInsufficient) Error() string { return "insufficient director points" }

type DirectorRepository struct {
	db        *gorm.DB
	maxActive int64
}

func NewDirectorRepository(db *gorm.DB, max int) *DirectorRepository {
	if max < 1 {
		max = 1
	}
	return &DirectorRepository{db, int64(max)}
}
func directorLock(tx *gorm.DB, user string) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "director:"+user).Error
}
func directorReplay(tx *gorm.DB, user, key, hash string) (*models.DirectorProject, error) {
	if key == "" {
		return nil, nil
	}
	var r models.DirectorReceipt
	e := tx.Where("user_id=? AND key=?", user, key).First(&r).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if r.Hash != hash {
		return nil, ErrDirectorIdempotency
	}
	// Deleted projects cannot be resurrected by replay.
	var live models.DirectorProject
	if e = tx.Where("id=? AND user_id=?", r.ProjectID, user).First(&live).Error; e != nil {
		return nil, e
	}
	live.Document = r.Response
	return &live, nil
}
func (r *DirectorRepository) Create(c context.Context, p *models.DirectorProject, key, hash string) (*models.DirectorProject, error) {
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := directorLock(tx, p.UserID.String()); e != nil {
			return e
		}
		old, e := directorReplay(tx, p.UserID.String(), key, hash)
		if e != nil {
			return e
		}
		if old != nil {
			*p = *old
			return nil
		}
		if e = tx.Create(p).Error; e != nil {
			return e
		}
		return tx.Create(&models.DirectorReceipt{UserID: p.UserID, Key: key, Hash: hash, ProjectID: p.ID, Response: p.Document, CreatedAt: time.Now()}).Error
	})
	return p, wrapErr("director create", e)
}
func (r *DirectorRepository) Get(c context.Context, id, user string) (*models.DirectorProject, error) {
	var p models.DirectorProject
	e := r.db.WithContext(c).Where("id=? AND user_id=?", id, user).First(&p).Error
	return &p, wrapErr("director get", e)
}
func (r *DirectorRepository) List(c context.Context, user string, page, limit int, search string) ([]models.DirectorProject, int64, error) {
	q := r.db.WithContext(c).Model(&models.DirectorProject{}).Where("user_id=?", user)
	if search != "" {
		q = q.Where("position(lower(?) in lower(name)) > 0", search)
	}
	var n int64
	if e := q.Count(&n).Error; e != nil {
		return nil, 0, e
	}
	items := []models.DirectorProject{}
	e := q.Order("updated_at DESC,id DESC").Offset((page - 1) * limit).Limit(limit).Find(&items).Error
	return items, n, e
}
func (r *DirectorRepository) Change(c context.Context, id, user, key, hash string, fn func(*models.DirectorProject, func(string) (*models.LibraryAsset, error)) ([]models.DirectorJob, error)) (*models.DirectorProject, error) {
	var p models.DirectorProject
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := directorLock(tx, user); e != nil {
			return e
		}
		old, e := directorReplay(tx, user, key, hash)
		if e != nil {
			return e
		}
		if old != nil {
			if old.ID.String() != id {
				return ErrDirectorIdempotency
			}
			p = *old
			return nil
		}
		if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", id, user).First(&p).Error; e != nil {
			return e
		}
		loadAsset := func(assetID string) (*models.LibraryAsset, error) {
			var asset models.LibraryAsset
			err := tx.Where("id=? AND user_id=?", assetID, user).First(&asset).Error
			return &asset, wrapErr("director owned asset", err)
		}
		jobs, e := fn(&p, loadAsset)
		if e != nil {
			return e
		}
		cost := 0
		for _, j := range jobs {
			cost += j.Cost
		}
		if len(jobs) > 0 {
			var n int64
			if e = tx.Model(&models.DirectorJob{}).Where("user_id=? AND status IN ?", user, []string{"queued", "submitting", "waiting"}).Distinct("project_id").Count(&n).Error; e != nil {
				return e
			}
			var same int64
			if e = tx.Model(&models.DirectorJob{}).Where("project_id=? AND status IN ?", p.ID, []string{"queued", "submitting", "waiting"}).Count(&same).Error; e != nil {
				return e
			}
			if same == 0 && n >= r.maxActive {
				return ErrConflict
			}
			a := models.DirectorAccount{UserID: p.UserID}
			if e = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&a).Error; e != nil {
				return e
			}
			if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=?", user).First(&a).Error; e != nil {
				return e
			}
			if a.Balance < int64(cost) {
				return &DirectorInsufficient{cost, a.Balance}
			}
			if e = tx.Model(&a).Update("balance", a.Balance-int64(cost)).Error; e != nil {
				return e
			}
			for i := range jobs {
				j := &jobs[i]
				if j.ID == uuid.Nil {
					j.ID = uuid.New()
				}
				j.UserID = p.UserID
				j.ProjectID = p.ID
				if e = tx.Create(j).Error; e != nil {
					return e
				}
				if e = tx.Create(&models.DirectorPointEntry{ID: uuid.New(), UserID: p.UserID, JobID: j.ID, Kind: "charge", Amount: j.Cost, CreatedAt: time.Now()}).Error; e != nil {
					return e
				}
			}
		}
		p.WorkVersion++
		p.UpdatedAt = time.Now()
		if e = tx.Model(&p).Updates(map[string]any{"name": p.Name, "document": p.Document, "work_version": p.WorkVersion, "updated_at": p.UpdatedAt, "deleted_at": p.DeletedAt}).Error; e != nil {
			return e
		}
		if p.DeletedAt.Valid {
			// Queued requests were never sent; refund them. In-flight calls remain fenced and are reconciled by the worker.
			var queued []models.DirectorJob
			if e = tx.Where("project_id=? AND status=?", p.ID, "queued").Find(&queued).Error; e != nil {
				return e
			}
			for i := range queued {
				j := &queued[i]
				if e = directorRefund(tx, j); e != nil {
					return e
				}
				if e = tx.Model(j).Update("status", "cancelled").Error; e != nil {
					return e
				}
			}
		}
		if key != "" {
			return tx.Create(&models.DirectorReceipt{UserID: p.UserID, Key: key, Hash: hash, ProjectID: p.ID, Response: p.Document, CreatedAt: time.Now()}).Error
		}
		return nil
	})
	return &p, wrapErr("director change", e)
}
func directorRefund(tx *gorm.DB, j *models.DirectorJob) error {
	if j.Refunded {
		return nil
	}
	entry := models.DirectorPointEntry{ID: uuid.New(), UserID: j.UserID, JobID: j.ID, Kind: "refund", Amount: j.Cost, CreatedAt: time.Now()}
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&entry)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 1 {
		if e := tx.Model(&models.DirectorAccount{}).Where("user_id=?", j.UserID).Update("balance", gorm.Expr("balance + ?", j.Cost)).Error; e != nil {
			return e
		}
	}
	j.Refunded = true
	return tx.Model(j).Update("refunded", true).Error
}
func (r *DirectorRepository) Claim(c context.Context) (*models.DirectorJob, error) {
	var j models.DirectorJob
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		// Lock the project first. All client writes and completions share that lock.
		var p models.DirectorProject
		if e := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("EXISTS (SELECT 1 FROM director_jobs j WHERE j.project_id=director_projects.id AND j.deleted_at IS NULL AND (j.status='queued' OR (j.status IN ('submitting','waiting') AND j.lease_until < ?))) AND NOT EXISTS (SELECT 1 FROM director_jobs a WHERE a.project_id=director_projects.id AND a.status='submitting' AND a.lease_until >= ?)", time.Now(), time.Now()).Order("updated_at,id").First(&p).Error; e != nil {
			return e
		}
		if e := tx.Where("project_id=? AND (status='queued' OR (status IN ('submitting','waiting') AND lease_until < ?))", p.ID, time.Now()).Order("CASE WHEN status='submitting' THEN 0 ELSE 1 END,created_at,id").First(&j).Error; e != nil {
			return e
		}
		token := uuid.New()
		until := time.Now().Add(12 * time.Minute)
		j.LeaseToken = &token
		j.LeaseUntil = &until
		// Preserve old status for worker recovery: expired submitting is never submitted again.
		previousStatus := j.Status
		err := tx.Model(&j).Updates(map[string]any{"status": "submitting", "lease_token": token, "lease_until": until, "updated_at": time.Now()}).Error
		j.Status = previousStatus
		return err
	})
	return &j, wrapErr("director claim", e)
}
func (r *DirectorRepository) Complete(c context.Context, j *models.DirectorJob, status string, refund bool, fn func(*models.DirectorProject) error) error {
	return wrapErr("director complete", r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := directorLock(tx, j.UserID.String()); e != nil {
			return e
		}
		var p models.DirectorProject
		if e := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", j.ProjectID, j.UserID).First(&p).Error; e != nil {
			return e
		}
		var current models.DirectorJob
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND lease_token=? AND status=?", j.ID, j.LeaseToken, "submitting").First(&current).Error; e != nil {
			return e
		}
		if refund {
			if e := directorRefund(tx, &current); e != nil {
				return e
			}
		}
		if !p.DeletedAt.Valid {
			if e := fn(&p); e != nil {
				return e
			}
			p.WorkVersion++
			if e := tx.Model(&p).Updates(map[string]any{"document": p.Document, "work_version": p.WorkVersion, "updated_at": time.Now()}).Error; e != nil {
				return e
			}
		}
		return tx.Model(&current).Updates(map[string]any{"status": status, "lease_token": nil, "lease_until": nil, "output": nil, "updated_at": time.Now()}).Error
	}))
}

func (r *DirectorRepository) Progress(c context.Context, j *models.DirectorJob, fn func(*models.DirectorProject) error) error {
	return wrapErr("director progress", r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := directorLock(tx, j.UserID.String()); e != nil {
			return e
		}
		var p models.DirectorProject
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", j.ProjectID, j.UserID).First(&p).Error; e != nil {
			return e
		}
		var n int64
		if e := tx.Model(&models.DirectorJob{}).Where("id=? AND lease_token=? AND status=?", j.ID, j.LeaseToken, "submitting").Count(&n).Error; e != nil {
			return e
		}
		if n != 1 {
			return ErrConflict
		}
		if e := fn(&p); e != nil {
			return e
		}
		p.WorkVersion++
		return tx.Model(&p).Updates(map[string]any{"document": p.Document, "work_version": p.WorkVersion, "updated_at": time.Now()}).Error
	}))
}

func (r *DirectorRepository) RecordOutput(c context.Context, j *models.DirectorJob, data []byte) error {
	q := r.db.WithContext(c).Model(&models.DirectorJob{}).Where("id=? AND user_id=? AND lease_token=? AND status=?", j.ID, j.UserID, j.LeaseToken, "submitting").Updates(map[string]any{"output": data, "snapshot": j.Snapshot})
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

// Wait releases the lease of an accepted asynchronous provider task. The stored output is the
// provider handle, so a later claim polls it instead of submitting the paid request again.
func (r *DirectorRepository) Wait(c context.Context, j *models.DirectorJob, output []byte, until time.Time) error {
	q := r.db.WithContext(c).Model(&models.DirectorJob{}).Where("id=? AND user_id=? AND lease_token=? AND status=?", j.ID, j.UserID, j.LeaseToken, "submitting").Updates(map[string]any{"status": "waiting", "output": output, "snapshot": j.Snapshot, "lease_token": nil, "lease_until": until, "updated_at": time.Now()})
	if q.Error != nil {
		return wrapErr("director wait", q.Error)
	}
	if q.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}
