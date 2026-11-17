package repository_open_api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Film (สร้างหนัง) persistence. It never writes Director tables except the shared point wallet
// (director_point_accounts); Director series and project rows are only read and row-locked.

var (
	ErrFilmIdempotency = errors.New("film idempotency conflict")
	ErrFilmDuplicate   = errors.New("film element name already used")
)

const (
	filmSeriesMode   = "film"
	filmLeaseTime    = 12 * time.Minute
	filmMaxActiveJob = 30
)

var filmActiveJobStatuses = []string{"queued", "submitting", "waiting"}

// FilmAssetLoader reads one library asset owned by the caller.
type FilmAssetLoader = func(string) (*models.LibraryAsset, error)

type FilmRepository struct {
	db *gorm.DB
}

func NewFilmRepository(db *gorm.DB) *FilmRepository { return &FilmRepository{db: db} }

func filmLock(tx *gorm.DB, user string) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "film:"+user).Error
}

func filmSeries(tx *gorm.DB, id, user string) (*models.DirectorSeries, error) {
	var s models.DirectorSeries
	e := tx.Where("id=? AND user_id=? AND mode=?", id, user, filmSeriesMode).First(&s).Error
	return &s, e
}

func filmAssetLoader(tx *gorm.DB, user string) FilmAssetLoader {
	return func(id string) (*models.LibraryAsset, error) {
		var a models.LibraryAsset
		e := tx.Where("id=? AND user_id=?", id, user).First(&a).Error
		return &a, wrapErr("film owned asset", e)
	}
}

// filmReplay returns the target of an earlier request with the same key; a different body is a conflict.
func filmReplay(tx *gorm.DB, user, key, hash string) (string, bool, error) {
	if key == "" {
		return "", false, nil
	}
	var r models.FilmReceipt
	e := tx.Where("user_id=? AND key=?", user, key).First(&r).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if e != nil {
		return "", false, e
	}
	if r.Hash != hash {
		return "", false, ErrFilmIdempotency
	}
	return r.TargetID, true, nil
}

func filmReceipt(tx *gorm.DB, user uuid.UUID, key, hash, target string) error {
	if key == "" {
		return nil
	}
	return tx.Create(&models.FilmReceipt{UserID: user, Key: key, Hash: hash, TargetID: target, CreatedAt: time.Now()}).Error
}

func filmDuplicate(e error) bool {
	return e != nil && (errors.Is(e, gorm.ErrDuplicatedKey) || strings.Contains(e.Error(), "film_elements_name") || strings.Contains(e.Error(), "SQLSTATE 23505"))
}

// filmCharge debits the shared wallet and records the queued jobs with their charge entries.
func filmCharge(tx *gorm.DB, user uuid.UUID, jobs []models.FilmJob) error {
	if len(jobs) == 0 {
		return nil
	}
	var active int64
	if e := tx.Model(&models.FilmJob{}).Where("user_id=? AND status IN ?", user, filmActiveJobStatuses).Count(&active).Error; e != nil {
		return e
	}
	if active+int64(len(jobs)) > filmMaxActiveJob {
		return ErrConflict
	}
	cost := 0
	for _, j := range jobs {
		cost += j.Cost
	}
	a := models.DirectorAccount{UserID: user}
	if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&a).Error; e != nil {
		return e
	}
	if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id=?", user).First(&a).Error; e != nil {
		return e
	}
	if a.Balance < int64(cost) {
		return &DirectorInsufficient{Required: cost, Balance: a.Balance}
	}
	if e := tx.Model(&models.DirectorAccount{}).Where("user_id=?", user).Update("balance", a.Balance-int64(cost)).Error; e != nil {
		return e
	}
	now := time.Now()
	for i := range jobs {
		j := &jobs[i]
		if j.ID == uuid.Nil {
			j.ID = uuid.New()
		}
		j.UserID, j.Status, j.CreatedAt, j.UpdatedAt = user, "queued", now, now
		if e := tx.Create(j).Error; e != nil {
			return e
		}
		if e := tx.Create(&models.FilmPointEntry{ID: uuid.New(), UserID: user, JobID: j.ID, Kind: "charge", Amount: j.Cost, CreatedAt: now}).Error; e != nil {
			return e
		}
	}
	return nil
}

func filmRefund(tx *gorm.DB, j *models.FilmJob) error {
	if j.Refunded {
		return nil
	}
	entry := models.FilmPointEntry{ID: uuid.New(), UserID: j.UserID, JobID: j.ID, Kind: "refund", Amount: j.Cost, CreatedAt: time.Now()}
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&entry)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 1 && j.Cost > 0 {
		if e := tx.Model(&models.DirectorAccount{}).Where("user_id=?", j.UserID).Update("balance", gorm.Expr("balance + ?", j.Cost)).Error; e != nil {
			return e
		}
	}
	j.Refunded = true
	return tx.Model(&models.FilmJob{}).Where("id=?", j.ID).Update("refunded", true).Error
}

// filmCancelQueued refunds and cancels jobs that were never sent to a provider.
func filmCancelQueued(tx *gorm.DB, target string) error {
	var queued []models.FilmJob
	if e := tx.Where("target_id=? AND status=?", target, "queued").Find(&queued).Error; e != nil {
		return e
	}
	for i := range queued {
		if e := filmRefund(tx, &queued[i]); e != nil {
			return e
		}
		if e := tx.Model(&models.FilmJob{}).Where("id=?", queued[i].ID).Updates(map[string]any{"status": "cancelled", "updated_at": time.Now()}).Error; e != nil {
			return e
		}
	}
	return nil
}

// ---------- Reads ----------

func (r *FilmRepository) Series(c context.Context, id, user string) (*models.DirectorSeries, error) {
	s, e := filmSeries(r.db.WithContext(c), id, user)
	return s, wrapErr("film series", e)
}

func (r *FilmRepository) Elements(c context.Context, seriesID, user string) ([]models.FilmElement, error) {
	items := []models.FilmElement{}
	e := r.db.WithContext(c).Where("series_id=? AND user_id=?", seriesID, user).Order("kind,created_at,id").Find(&items).Error
	return items, wrapErr("film elements", e)
}

func (r *FilmRepository) Element(c context.Context, id, user string) (*models.FilmElement, error) {
	var el models.FilmElement
	e := r.db.WithContext(c).Where("id=? AND user_id=?", id, user).First(&el).Error
	return &el, wrapErr("film element", e)
}

// Episode loads an episode of a film series with its film document, creating an empty document
// the first time. Episodes of Director (non-film) series are reported as not found.
func (r *FilmRepository) Episode(c context.Context, projectID, user string) (*models.DirectorProject, *models.DirectorSeries, *models.FilmEpisode, error) {
	db := r.db.WithContext(c)
	var p models.DirectorProject
	if e := db.Where("id=? AND user_id=? AND series_id IS NOT NULL", projectID, user).First(&p).Error; e != nil {
		return nil, nil, nil, wrapErr("film episode project", e)
	}
	s, e := filmSeries(db, p.SeriesID.String(), user)
	if e != nil {
		return nil, nil, nil, wrapErr("film episode series", e)
	}
	ep, e := filmEnsureEpisode(db, &p, false)
	return &p, s, ep, wrapErr("film episode", e)
}

func filmEnsureEpisode(tx *gorm.DB, p *models.DirectorProject, lock bool) (*models.FilmEpisode, error) {
	now := time.Now()
	seed := models.FilmEpisode{ProjectID: p.ID, UserID: p.UserID, Document: []byte(`{"shots":[]}`), Revision: 1, CreatedAt: now, UpdatedAt: now}
	if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; e != nil {
		return nil, e
	}
	var ep models.FilmEpisode
	q := tx
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	e := q.Where("project_id=?", p.ID).First(&ep).Error
	return &ep, e
}

// ---------- Client writes ----------

func (r *FilmRepository) CreateElement(c context.Context, el *models.FilmElement, key, hash string, validate func(*models.FilmElement, FilmAssetLoader) error) (*models.FilmElement, error) {
	user := el.UserID.String()
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := filmLock(tx, user); e != nil {
			return e
		}
		target, found, e := filmReplay(tx, user, key, hash)
		if e != nil {
			return e
		}
		if found {
			var old models.FilmElement
			if e = tx.Where("id=? AND user_id=?", target, user).First(&old).Error; e != nil {
				return e
			}
			*el = old
			return nil
		}
		if _, e = filmSeries(tx, el.SeriesID.String(), user); e != nil {
			return e
		}
		if e = validate(el, filmAssetLoader(tx, user)); e != nil {
			return e
		}
		if e = tx.Create(el).Error; e != nil {
			if filmDuplicate(e) {
				return ErrFilmDuplicate
			}
			return e
		}
		return filmReceipt(tx, el.UserID, key, hash, el.ID.String())
	})
	return el, filmWrap("film element create", e)
}

// ChangeElement edits an element under the owner's film lock. fn may return jobs to charge and queue.
func (r *FilmRepository) ChangeElement(c context.Context, id, user, key, hash string, fn func(*models.FilmElement, *models.DirectorSeries, FilmAssetLoader) ([]models.FilmJob, error)) (*models.FilmElement, error) {
	var el models.FilmElement
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := filmLock(tx, user); e != nil {
			return e
		}
		target, found, e := filmReplay(tx, user, key, hash)
		if e != nil {
			return e
		}
		if found && target != id {
			return ErrFilmIdempotency
		}
		if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", id, user).First(&el).Error; e != nil {
			return e
		}
		if found {
			return nil
		}
		s, e := filmSeries(tx, el.SeriesID.String(), user)
		if e != nil {
			return e
		}
		jobs, e := fn(&el, s, filmAssetLoader(tx, user))
		if e != nil {
			return e
		}
		if e = filmCharge(tx, el.UserID, jobs); e != nil {
			return e
		}
		el.UpdatedAt = time.Now()
		if e = tx.Save(&el).Error; e != nil {
			if filmDuplicate(e) {
				return ErrFilmDuplicate
			}
			return e
		}
		if el.DeletedAt.Valid {
			if e = filmCancelQueued(tx, "element:"+el.ID.String()); e != nil {
				return e
			}
		}
		return filmReceipt(tx, el.UserID, key, hash, id)
	})
	return &el, filmWrap("film element change", e)
}

// ChangeEpisode edits the film document of an episode. Worker-owned video state lives in the same
// document, so only client writes advance the revision.
func (r *FilmRepository) ChangeEpisode(c context.Context, projectID, user, key, hash string, fn func(*models.FilmEpisode, *models.DirectorProject, *models.DirectorSeries, []models.FilmElement) ([]models.FilmJob, error)) (*models.FilmEpisode, error) {
	var ep *models.FilmEpisode
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := filmLock(tx, user); e != nil {
			return e
		}
		target, found, e := filmReplay(tx, user, key, hash)
		if e != nil {
			return e
		}
		if found && target != projectID {
			return ErrFilmIdempotency
		}
		var p models.DirectorProject
		if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=? AND series_id IS NOT NULL", projectID, user).First(&p).Error; e != nil {
			return e
		}
		s, e := filmSeries(tx, p.SeriesID.String(), user)
		if e != nil {
			return e
		}
		if ep, e = filmEnsureEpisode(tx, &p, true); e != nil {
			return e
		}
		if found {
			return nil
		}
		elements := []models.FilmElement{}
		if e = tx.Where("series_id=? AND user_id=?", s.ID, user).Order("kind,created_at,id").Find(&elements).Error; e != nil {
			return e
		}
		jobs, e := fn(ep, &p, s, elements)
		if e != nil {
			return e
		}
		if e = filmCharge(tx, p.UserID, jobs); e != nil {
			return e
		}
		ep.Revision++
		ep.UpdatedAt = time.Now()
		if e = tx.Model(&models.FilmEpisode{}).Where("project_id=?", ep.ProjectID).Updates(map[string]any{"document": ep.Document, "revision": ep.Revision, "updated_at": ep.UpdatedAt}).Error; e != nil {
			return e
		}
		return filmReceipt(tx, p.UserID, key, hash, projectID)
	})
	return ep, filmWrap("film episode change", e)
}

func filmWrap(op string, e error) error {
	var points *DirectorInsufficient
	if e == nil || errors.Is(e, ErrFilmDuplicate) || errors.Is(e, ErrFilmIdempotency) || errors.Is(e, ErrConflict) || errors.As(e, &points) {
		return e
	}
	return wrapErr(op, e)
}

// ---------- Worker ----------

// Claim leases the oldest runnable film job. Status keeps its previous value in the returned
// copy so the worker can tell a fresh job from an expired submission.
func (r *FilmRepository) Claim(c context.Context) (*models.FilmJob, error) {
	var j models.FilmJob
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status='queued' OR (status IN ('submitting','waiting') AND lease_until < ?)", now).
			Order("created_at,id").First(&j).Error; e != nil {
			return e
		}
		token, until := uuid.New(), now.Add(filmLeaseTime)
		previous := j.Status
		if e := tx.Model(&models.FilmJob{}).Where("id=?", j.ID).Updates(map[string]any{"status": "submitting", "lease_token": token, "lease_until": until, "updated_at": now}).Error; e != nil {
			return e
		}
		j.LeaseToken, j.LeaseUntil, j.Status = &token, &until, previous
		return nil
	})
	return &j, wrapErr("film claim", e)
}

func (r *FilmRepository) leased(tx *gorm.DB, j *models.FilmJob) *gorm.DB {
	return tx.Model(&models.FilmJob{}).Where("id=? AND lease_token=? AND status=?", j.ID, j.LeaseToken, "submitting")
}

func (r *FilmRepository) RecordOutput(c context.Context, j *models.FilmJob, data []byte) error {
	q := r.leased(r.db.WithContext(c), j).Updates(map[string]any{"output": data, "updated_at": time.Now()})
	if q.Error != nil {
		return wrapErr("film output", q.Error)
	}
	if q.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

// Wait releases the lease until the next poll; the stored output is the provider handle.
func (r *FilmRepository) Wait(c context.Context, j *models.FilmJob, output []byte, until time.Time) error {
	q := r.leased(r.db.WithContext(c), j).Updates(map[string]any{"status": "waiting", "output": output, "lease_token": nil, "lease_until": until, "updated_at": time.Now()})
	if q.Error != nil {
		return wrapErr("film wait", q.Error)
	}
	if q.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

func (r *FilmRepository) finish(tx *gorm.DB, j *models.FilmJob, status string, refund bool) error {
	var current models.FilmJob
	if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND lease_token=? AND status=?", j.ID, j.LeaseToken, "submitting").First(&current).Error; e != nil {
		return e
	}
	if refund {
		if e := filmRefund(tx, &current); e != nil {
			return e
		}
	}
	return tx.Model(&models.FilmJob{}).Where("id=?", j.ID).Updates(map[string]any{"status": status, "lease_token": nil, "lease_until": nil, "output": nil, "updated_at": time.Now()}).Error
}

// CompleteElement ends an element job; fn runs only while the element still exists.
func (r *FilmRepository) CompleteElement(c context.Context, j *models.FilmJob, elementID, status string, refund bool, fn func(*models.FilmElement) error) error {
	return wrapErr("film element complete", r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := filmLock(tx, j.UserID.String()); e != nil {
			return e
		}
		if e := r.finish(tx, j, status, refund); e != nil {
			return e
		}
		var el models.FilmElement
		e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", elementID, j.UserID).First(&el).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		if e = fn(&el); e != nil {
			return e
		}
		el.UpdatedAt = time.Now()
		return tx.Save(&el).Error
	}))
}

// CompleteEpisode ends a shot job; fn runs only while the episode still exists.
func (r *FilmRepository) CompleteEpisode(c context.Context, j *models.FilmJob, status string, refund bool, fn func(*models.FilmEpisode) error) error {
	return wrapErr("film episode complete", r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := filmLock(tx, j.UserID.String()); e != nil {
			return e
		}
		if e := r.finish(tx, j, status, refund); e != nil {
			return e
		}
		return r.episodeWrite(tx, j, fn)
	}))
}

// ProgressEpisode updates worker-owned state while the job still holds its lease.
func (r *FilmRepository) ProgressEpisode(c context.Context, j *models.FilmJob, fn func(*models.FilmEpisode) error) error {
	return filmWrap("film episode progress", r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := filmLock(tx, j.UserID.String()); e != nil {
			return e
		}
		var n int64
		if e := r.leased(tx, j).Count(&n).Error; e != nil {
			return e
		}
		if n != 1 {
			return ErrConflict
		}
		return r.episodeWrite(tx, j, fn)
	}))
}

func (r *FilmRepository) episodeWrite(tx *gorm.DB, j *models.FilmJob, fn func(*models.FilmEpisode) error) error {
	if j.ProjectID == nil {
		return nil
	}
	var p models.DirectorProject
	e := tx.Where("id=? AND user_id=?", *j.ProjectID, j.UserID).First(&p).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	ep, e := filmEnsureEpisode(tx, &p, true)
	if e != nil {
		return e
	}
	if e = fn(ep); e != nil {
		return e
	}
	return tx.Model(&models.FilmEpisode{}).Where("project_id=?", ep.ProjectID).Updates(map[string]any{"document": ep.Document, "updated_at": time.Now()}).Error
}

// MarkElementProcessing moves a queued element to processing when its image job starts.
func (r *FilmRepository) MarkElementProcessing(c context.Context, j *models.FilmJob, elementID string) error {
	e := r.db.WithContext(c).Model(&models.FilmElement{}).Where("id=? AND user_id=? AND image_status=?", elementID, j.UserID, "queued").Updates(map[string]any{"image_status": "processing", "updated_at": time.Now()}).Error
	return wrapErr("film element processing", e)
}

// CompletePlan finishes a plan job in one transaction: fn receives the episode and the series'
// elements, rewrites the document and returns new elements to insert. An fn error rolls back.
func (r *FilmRepository) CompletePlan(c context.Context, j *models.FilmJob, fn func(*models.FilmEpisode, []models.FilmElement, *models.DirectorSeries) ([]models.FilmElement, error)) error {
	return filmWrap("film plan complete", r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := filmLock(tx, j.UserID.String()); e != nil {
			return e
		}
		if e := r.finish(tx, j, "succeeded", false); e != nil {
			return e
		}
		if j.ProjectID == nil {
			return nil
		}
		var p models.DirectorProject
		if e := tx.Where("id=? AND user_id=?", *j.ProjectID, j.UserID).First(&p).Error; e != nil {
			return e
		}
		s, e := filmSeries(tx, j.SeriesID.String(), j.UserID.String())
		if e != nil {
			return e
		}
		ep, e := filmEnsureEpisode(tx, &p, true)
		if e != nil {
			return e
		}
		elements := []models.FilmElement{}
		if e = tx.Where("series_id=? AND user_id=?", j.SeriesID, j.UserID).Order("kind,created_at,id").Find(&elements).Error; e != nil {
			return e
		}
		created, e := fn(ep, elements, s)
		if e != nil {
			return e
		}
		for i := range created {
			if e = tx.Create(&created[i]).Error; e != nil {
				if filmDuplicate(e) {
					return ErrFilmDuplicate
				}
				return e
			}
		}
		return tx.Model(&models.FilmEpisode{}).Where("project_id=?", ep.ProjectID).Updates(map[string]any{"document": ep.Document, "updated_at": time.Now()}).Error
	}))
}

// Balance reads the shared point wallet.
func (r *FilmRepository) Balance(c context.Context, user string) (int64, error) {
	var a models.DirectorAccount
	e := r.db.WithContext(c).Where("user_id=?", user).First(&a).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	return a.Balance, wrapErr("film balance", e)
}
