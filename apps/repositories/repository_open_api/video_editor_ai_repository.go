package repository_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

var (
	ErrAIState          = errors.New("AI job state conflict")
	ErrAIBusy           = errors.New("AI job active")
	ErrAIDaily          = errors.New("AI daily limit")
	ErrAISelection      = errors.New("AI concept selected")
	ErrAIDraft          = errors.New("AI draft conflict")
	ErrAIAlreadyApplied = errors.New("AI already applied")
	ErrAIRetry          = errors.New("AI retry not ready")
	ErrAIBudget         = errors.New("AI cloud budget exhausted")
)

type VideoEditorAIRepository struct{ db *gorm.DB }

func NewVideoEditorAIRepository(db *gorm.DB) *VideoEditorAIRepository {
	return &VideoEditorAIRepository{db: db}
}
func aiUserLock(tx *gorm.DB, user uuid.UUID) error {
	var u models.User
	return veError(tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id=?", user).First(&u).Error)
}
func (r *VideoEditorAIRepository) Existing(ctx context.Context, user, request string) (*models.VideoEditorAIJob, error) {
	var j models.VideoEditorAIJob
	e := r.db.WithContext(ctx).Where("user_id=? AND request_id=?", user, request).First(&j).Error
	return &j, veError(e)
}
func (r *VideoEditorAIRepository) Get(ctx context.Context, id, user string) (*models.VideoEditorAIJob, error) {
	var j models.VideoEditorAIJob
	e := r.db.WithContext(ctx).Where("id=? AND user_id=?", id, user).First(&j).Error
	return &j, veError(e)
}
func (r *VideoEditorAIRepository) List(ctx context.Context, project, user string) ([]models.VideoEditorAIJob, error) {
	out := []models.VideoEditorAIJob{}
	e := r.db.WithContext(ctx).Where("project_id=? AND user_id=?", project, user).Order("created_at DESC,id DESC").Limit(20).Find(&out).Error
	return out, e
}
func (r *VideoEditorAIRepository) Create(ctx context.Context, j *models.VideoEditorAIJob) (bool, error) {
	created := false
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := aiUserLock(tx, j.UserID); e != nil {
			return e
		}
		var old models.VideoEditorAIJob
		e := tx.Where("user_id=? AND request_id=?", j.UserID, j.RequestID).First(&old).Error
		if e == nil {
			if old.RequestHash != j.RequestHash {
				return ErrVEIdempotency
			}
			*j = old
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		var p models.VideoEditorProject
		if e := tx.Where("id=? AND user_id=?", j.ProjectID, j.UserID).First(&p).Error; e != nil {
			return veError(e)
		}
		if p.Revision != j.SourceRevision {
			return ErrVERevision
		}
		var n int64
		if e := tx.Model(&models.VideoEditorAIJob{}).Where("user_id=? AND status IN ('queued','processing')", j.UserID).Count(&n).Error; e != nil {
			return e
		}
		if n > 0 {
			return ErrAIBusy
		}
		now := time.Now().UTC()
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		if e := tx.Model(&models.VideoEditorAIJob{}).Where("user_id=? AND created_at>=?", j.UserID, day).Count(&n).Error; e != nil {
			return e
		}
		if n >= 20 {
			return ErrAIDaily
		}
		if e := tx.Model(&models.VideoEditorAIJob{}).Where("user_id=? AND project_id=? AND status IN ('queued','processing','awaiting_concept','review')", j.UserID, j.ProjectID).Count(&n).Error; e != nil {
			return e
		}
		if n >= 5 {
			return ErrAIBusy
		}
		if e := tx.Create(j).Error; e != nil {
			return e
		}
		created = true
		return nil
	})
	return created, e
}
func (r *VideoEditorAIRepository) Select(ctx context.Context, id, user, concept string) (*models.VideoEditorAIJob, error) {
	var j models.VideoEditorAIJob
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		u, e := uuid.Parse(user)
		if e != nil {
			return ErrNotFound
		}
		if e = aiUserLock(tx, u); e != nil {
			return e
		}
		if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", id, user).First(&j).Error; e != nil {
			return veError(e)
		}
		if j.SelectedConceptID != nil {
			if j.SelectedConceptID.String() == concept {
				return nil
			}
			return ErrAISelection
		}
		if j.Status != "awaiting_concept" {
			return ErrAIState
		}
		concepts := []models.EditorAIConcept{}
		if json.Unmarshal(j.Concepts, &concepts) != nil {
			return ErrAIState
		}
		found := false
		for _, c := range concepts {
			if c.ID == concept {
				found = true
			}
		}
		if !found {
			return ErrAISelection
		}
		var n int64
		if e = tx.Model(&models.VideoEditorAIJob{}).Where("user_id=? AND status IN ('queued','processing')", user).Count(&n).Error; e != nil {
			return e
		}
		if n > 0 {
			return ErrAIBusy
		}
		cid := uuid.MustParse(concept)
		return tx.Model(&j).Updates(map[string]any{"selected_concept_id": cid, "phase": "planning", "status": "queued", "stage": "queued", "failure_code": "", "failure_message": "", "retry_at": nil, "updated_at": time.Now()}).Error
	})
	if e != nil {
		return nil, e
	}
	return r.Get(ctx, id, user)
}
func (r *VideoEditorAIRepository) UpdateDraft(ctx context.Context, id, user string, revision int, draft models.EditorAIDraft) (*models.VideoEditorAIJob, error) {
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var j models.VideoEditorAIJob
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", id, user).First(&j).Error; e != nil {
			return veError(e)
		}
		if j.Status != "review" {
			return ErrAIState
		}
		var old models.EditorAIDraft
		if json.Unmarshal(j.Draft, &old) != nil || old.Revision != revision {
			return ErrAIDraft
		}
		draft.Revision = revision + 1
		b, e := json.Marshal(draft)
		if e != nil {
			return e
		}
		return tx.Model(&j).Updates(map[string]any{"draft": datatypes.JSON(b), "updated_at": time.Now()}).Error
	})
	if e != nil {
		return nil, e
	}
	return r.Get(ctx, id, user)
}
func (r *VideoEditorAIRepository) Apply(ctx context.Context, id, user, request string, revision, draftRevision int, approved []string) (*models.VideoEditorProject, error) {
	var result models.VideoEditorProject
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var lookup models.VideoEditorAIJob
		if e := tx.Where("id=? AND user_id=?", id, user).First(&lookup).Error; e != nil {
			return veError(e)
		}
		var p models.VideoEditorProject
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", lookup.ProjectID, user).First(&p).Error; e != nil {
			return veError(e)
		}
		var j models.VideoEditorAIJob
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", id, user).First(&j).Error; e != nil {
			return veError(e)
		}
		if j.Status == "applied" {
			if j.AppliedRequestID == nil || j.AppliedRequestID.String() != request {
				return ErrAIAlreadyApplied
			}
			if json.Unmarshal(j.AppliedProject, &result) != nil {
				return ErrAIState
			}
			return nil
		}
		if j.Status != "review" {
			return ErrAIState
		}
		if p.Revision != revision {
			return ErrVERevision
		}
		var d models.EditorAIDraft
		if json.Unmarshal(j.Draft, &d) != nil || d.Revision != draftRevision {
			return ErrAIDraft
		}
		if len(d.Segments) == 0 || len(approved) != len(d.Segments) {
			return ErrAIDraft
		}
		set := map[string]bool{}
		for _, a := range approved {
			if set[a] {
				return ErrAIDraft
			}
			set[a] = true
		}
		segs := []models.VideoEditorSegment{}
		for _, s := range d.Segments {
			if !set[s.ID] {
				return ErrAIDraft
			}
			segs = append(segs, models.VideoEditorSegment{ID: s.ID, AssetID: s.AssetID, Start: s.Start, End: s.End, Volume: s.Volume})
		}
		raw, e := json.Marshal(segs)
		if e != nil {
			return e
		}
		p.Segments = datatypes.JSON(raw)
		var input models.EditorAIInput
		if json.Unmarshal(j.Input, &input) != nil {
			return ErrAIState
		}
		if input.AutoCompose {
			var existingAudio []models.VideoEditorAudioClip
			var existingText []models.VideoEditorTextLayer
			if json.Unmarshal(p.AudioClips, &existingAudio) != nil || json.Unmarshal(p.TextLayers, &existingText) != nil {
				return ErrAIDraft
			}
			audio := []models.VideoEditorAudioClip{}
			text := []models.VideoEditorTextLayer{}
			for _, clip := range existingAudio {
				if _, parseErr := uuid.Parse(clip.OriginJobID); parseErr != nil {
					audio = append(audio, clip)
				}
			}
			for _, layer := range existingText {
				if _, parseErr := uuid.Parse(layer.OriginJobID); parseErr != nil {
					text = append(text, layer)
				}
			}
			if len(audio) == 0 {
				audio = append(audio, d.AudioClips...)
			}
			text = append(text, d.TextLayers...)
			if len(audio) > 20 || len(text) > 50 {
				return ErrAIDraft
			}
			a, err := json.Marshal(audio)
			if err != nil {
				return err
			}
			t, err := json.Marshal(text)
			if err != nil {
				return err
			}
			p.AudioClips = datatypes.JSON(a)
			p.TextLayers = datatypes.JSON(t)
		}
		p.Revision++
		p.UpdatedAt = time.Now().UTC()
		if e = tx.Model(&p).Updates(map[string]any{"segments": p.Segments, "audio_clips": p.AudioClips, "text_layers": p.TextLayers, "revision": p.Revision, "updated_at": p.UpdatedAt}).Error; e != nil {
			return e
		}
		applied, e := json.Marshal(p)
		if e != nil {
			return e
		}
		result = p
		return tx.Model(&j).Updates(map[string]any{"status": "applied", "stage": "applied", "applied_request_id": request, "applied_project": datatypes.JSON(applied), "attempt_token": nil, "lease_until": nil, "updated_at": time.Now()}).Error
	})
	return &result, e
}
func (r *VideoEditorAIRepository) Cancel(ctx context.Context, id, user string) (*models.VideoEditorAIJob, error) {
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var j models.VideoEditorAIJob
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", id, user).First(&j).Error; e != nil {
			return veError(e)
		}
		if j.Status == "cancelled" {
			return nil
		}
		if j.Status == "applied" || j.Status == "failed" {
			return ErrAIState
		}
		return tx.Model(&j).Updates(map[string]any{"status": "cancelled", "stage": "cancelled", "attempt_token": nil, "lease_until": nil, "retry_at": nil, "updated_at": time.Now()}).Error
	})
	if e != nil {
		return nil, e
	}
	return r.Get(ctx, id, user)
}
func (r *VideoEditorAIRepository) Retry(ctx context.Context, id, user, request string) (*models.VideoEditorAIJob, error) {
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		u, e := uuid.Parse(user)
		if e != nil {
			return ErrNotFound
		}
		if e = aiUserLock(tx, u); e != nil {
			return e
		}
		var j models.VideoEditorAIJob
		if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", id, user).First(&j).Error; e != nil {
			return veError(e)
		}
		ids := []string{}
		_ = json.Unmarshal(j.RetryRequests, &ids)
		for _, v := range ids {
			if v == request {
				return nil
			}
		}
		attempts := j.AnalysisAttempts
		if j.Phase == "planning" {
			attempts = j.PlanningAttempts
		}
		if j.FailureCode == "AI_INVALID_RESULT" || j.FailureCode == "AI_INPUT_TOO_LARGE" || j.Status != "failed" || j.CloudCallsUsed >= 6 || attempts >= 3 || j.RetryAt != nil && j.RetryAt.After(time.Now()) {
			return ErrAIRetry
		}
		var n int64
		if e = tx.Model(&models.VideoEditorAIJob{}).Where("user_id=? AND status IN ('queued','processing')", user).Count(&n).Error; e != nil {
			return e
		}
		if n > 0 {
			return ErrAIBusy
		}
		ids = append(ids, request)
		raw, _ := json.Marshal(ids)
		return tx.Model(&j).Updates(map[string]any{"status": "queued", "stage": "queued", "retry_requests": datatypes.JSON(raw), "failure_code": "", "failure_message": "", "retry_at": nil, "updated_at": time.Now()}).Error
	})
	if e != nil {
		return nil, e
	}
	return r.Get(ctx, id, user)
}
func (r *VideoEditorAIRepository) Expire(ctx context.Context) error {
	return r.db.WithContext(ctx).Model(&models.VideoEditorAIJob{}).Where("status='processing' AND lease_until<?", time.Now()).Updates(map[string]any{"status": "failed", "stage": "failed", "failure_code": "AI_PROVIDER_UNAVAILABLE", "failure_message": "งานหยุดชะงัก กรุณาลองใหม่", "attempt_token": nil, "lease_until": nil, "updated_at": time.Now()}).Error
}
func (r *VideoEditorAIRepository) Claim(ctx context.Context) (*models.VideoEditorAIJob, error) {
	var j models.VideoEditorAIJob
	e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status='queued'").Order("created_at,id").First(&j).Error; e != nil {
			return veError(e)
		}
		field := "analysis_attempts"
		n := j.AnalysisAttempts
		if j.Phase == "planning" {
			field = "planning_attempts"
			n = j.PlanningAttempts
		}
		if n >= 3 {
			return ErrAIRetry
		}
		token := uuid.New()
		until := time.Now().Add(20 * time.Minute)
		if e := tx.Model(&j).Updates(map[string]any{"status": "processing", "stage": "queued", "attempt_token": token, "lease_until": until, field: n + 1, "updated_at": time.Now()}).Error; e != nil {
			return e
		}
		j.AttemptToken = &token
		j.LeaseUntil = &until
		j.Status = "processing"
		if j.Phase == "planning" {
			j.PlanningAttempts++
		} else {
			j.AnalysisAttempts++
		}
		return nil
	})
	return &j, e
}
func aiClaim(tx *gorm.DB, j *models.VideoEditorAIJob) *gorm.DB {
	return tx.Model(&models.VideoEditorAIJob{}).Where("id=? AND user_id=? AND status='processing' AND attempt_token=? AND lease_until>?", j.ID, j.UserID, j.AttemptToken, time.Now())
}
func (r *VideoEditorAIRepository) UpdateClaim(ctx context.Context, j *models.VideoEditorAIJob, fields map[string]any) error {
	fields["updated_at"] = time.Now()
	res := aiClaim(r.db.WithContext(ctx), j).Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return ErrAIState
	}
	return nil
}
func (r *VideoEditorAIRepository) Live(ctx context.Context, j *models.VideoEditorAIJob) bool {
	live, err := r.ClaimLive(ctx, j)
	return err == nil && live
}
func (r *VideoEditorAIRepository) ClaimLive(ctx context.Context, j *models.VideoEditorAIJob) (bool, error) {
	var n int64
	err := aiClaim(r.db.WithContext(ctx), j).Count(&n).Error
	return n == 1, err
}
func (r *VideoEditorAIRepository) ReserveCall(ctx context.Context, j *models.VideoEditorAIJob) error {
	res := aiClaim(r.db.WithContext(ctx), j).Where("cloud_calls_used<6").Updates(map[string]any{"cloud_calls_used": gorm.Expr("cloud_calls_used+1"), "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		if !r.Live(ctx, j) {
			return ErrAIState
		}
		return ErrAIBudget
	}
	j.CloudCallsUsed++
	return nil
}
func (r *VideoEditorAIRepository) Cache(ctx context.Context, user, asset, hash, kind, signature string) (datatypes.JSON, error) {
	var c models.VideoEditorAICache
	e := r.db.WithContext(ctx).Where("user_id=? AND asset_id=? AND content_hash=? AND kind=? AND signature=?", user, asset, hash, kind, signature).First(&c).Error
	return c.Result, veError(e)
}
func (r *VideoEditorAIRepository) PublishCache(ctx context.Context, j *models.VideoEditorAIJob, c *models.VideoEditorAICache) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked models.VideoEditorAIJob
		if e := aiClaim(tx, j).Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked).Error; e != nil {
			return ErrAIState
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(c).Error
	})
}

func (r *VideoEditorAIRepository) Ready(ctx context.Context) bool {
	d := r.db.WithContext(ctx)
	return d.Migrator().HasTable(&models.VideoEditorAIJob{}) && d.Migrator().HasTable(&models.VideoEditorAICache{})
}
