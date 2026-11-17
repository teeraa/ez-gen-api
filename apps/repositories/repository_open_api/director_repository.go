package repository_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// maxParallel caps images (and, separately, videos) of one project running at the same time.
	maxParallel int64
}

func NewDirectorRepository(db *gorm.DB, max int) *DirectorRepository {
	if max < 1 {
		max = 1
	}
	return &DirectorRepository{db, int64(max), directorDefaultParallel}
}

const directorDefaultParallel = 6

// SetMaxParallel caps how many storyboard images, and separately how many shot videos, of one
// project run at the same time.
func (r *DirectorRepository) SetMaxParallel(n int) *DirectorRepository {
	if n < 1 {
		n = 1
	}
	r.maxParallel = int64(n)
	return r
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
	q := r.db.WithContext(c).Model(&models.DirectorProject{}).Where("user_id=? AND series_id IS NULL", user)
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
		if e = tx.Model(&p).Updates(map[string]any{"name": p.Name, "document": p.Document, "work_version": p.WorkVersion, "updated_at": p.UpdatedAt, "deleted_at": p.DeletedAt, "episode_title": p.EpisodeTitle}).Error; e != nil {
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

// Only the current unrefunded missing-frame generation may resume from its already stored video.
const directorRecoverableFrameSQL = `j.status='needs_attention' AND j.kind='video' AND j.refunded=false
 AND coalesce(j.snapshot->>'shotId','')<>'' AND j.target_id='video:'||(j.snapshot->>'shotId')
 AND EXISTS (SELECT 1 FROM director_projects rp WHERE rp.id=j.project_id AND rp.user_id=j.user_id AND rp.deleted_at IS NULL
 AND rp.document->'generations'->>j.target_id=j.snapshot->>'generation'
 AND EXISTS (SELECT 1 FROM jsonb_array_elements(coalesce(rp.document#>'{project,scenes}','[]'::jsonb)) scene
 CROSS JOIN LATERAL jsonb_array_elements(coalesce(scene->'shots','[]'::jsonb)) shot
 WHERE shot->>'id'=j.snapshot->>'shotId' AND shot#>>'{video,status}'='needs_attention'
 AND shot#>>'{video,failureCode}'='VIDEO_LAST_FRAME_MISSING'))`

// directorClaimableSQL selects a job (alias j) the worker may start now. Its placeholders are the
// current time three times, then the per-project parallel cap.
//
// Jobs of one project normally run one at a time. Storyboard images and shot videos are the
// exception: jobs of the same kind (image with image, video with video) run side by side, up to
// the cap per kind. Videos of one scene still chain, because a shot whose previous clip is not
// ready goes back to waiting (directorPreviousShot), so in practice the first shot of every scene
// starts at once. A parallel job waits for any running job of another kind and for any queued
// job of a serial kind (the storyboard plan that writes the shot descriptions comes first); a
// serial job waits for every running job of its project.
const directorClaimableSQL = `j.deleted_at IS NULL AND (j.status='queued' OR (j.status IN ('submitting','waiting') AND j.lease_until < ?) OR (` + directorRecoverableFrameSQL + `))
 AND NOT EXISTS (SELECT 1 FROM director_jobs a WHERE a.project_id=j.project_id AND a.id<>j.id AND (
  (a.status='submitting' AND a.lease_until >= ? AND (j.kind NOT IN ('image','video') OR a.kind<>j.kind))
  OR (j.kind IN ('image','video') AND a.kind NOT IN ('image','video') AND a.status='queued' AND a.deleted_at IS NULL)))
 AND (j.kind NOT IN ('image','video') OR (SELECT count(*) FROM director_jobs b WHERE b.project_id=j.project_id AND b.kind=j.kind AND b.status='submitting' AND b.lease_until >= ?) < ?)`

func (r *DirectorRepository) Claim(c context.Context) (*models.DirectorJob, error) {
	var j models.DirectorJob
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		// Lock the project first. All client writes and completions share that lock.
		var p models.DirectorProject
		now := time.Now()
		args := []any{now, now, now, r.maxParallel}
		if e := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("EXISTS (SELECT 1 FROM director_jobs j WHERE j.project_id=director_projects.id AND "+directorClaimableSQL+")", args...).Order("updated_at,id").First(&p).Error; e != nil {
			return e
		}
		if e := tx.Table("director_jobs AS j").Where("j.project_id=? AND "+directorClaimableSQL, append([]any{p.ID}, args...)...).Order("CASE WHEN j.status='submitting' THEN 0 ELSE 1 END,j.created_at,j.id").First(&j).Error; e != nil {
			return e
		}
		token := uuid.New()
		until := time.Now().Add(12 * time.Minute)
		j.LeaseToken = &token
		j.LeaseUntil = &until
		// Preserve old status for worker recovery: expired submitting is never submitted again.
		previousStatus := j.Status
		if previousStatus == "needs_attention" {
			var snapshot struct {
				ShotID string `json:"shotId"`
			}
			if e := json.Unmarshal(j.Snapshot, &snapshot); e != nil {
				return e
			}
			checkpoint := struct {
				OwnedVideoKey string `json:"ownedVideoKey"`
				Recovered     bool   `json:"recovered"`
			}{fmt.Sprintf("director/%s/%s/%s/%s.mp4", j.UserID, j.ProjectID, snapshot.ShotID, j.ID), true}
			var e error
			j.Output, e = json.Marshal(checkpoint)
			if e != nil {
				return e
			}
		}
		err := tx.Model(&j).Updates(map[string]any{"status": "submitting", "lease_token": token, "lease_until": until, "updated_at": time.Now(), "output": j.Output}).Error
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

// Balance reads the owner's Director point balance; an account that was never funded is zero.
func (r *DirectorRepository) Balance(c context.Context, user string) (int64, error) {
	var a models.DirectorAccount
	e := r.db.WithContext(c).Where("user_id=?", user).First(&a).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	return a.Balance, wrapErr("director balance", e)
}
