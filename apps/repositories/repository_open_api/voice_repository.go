package repository_open_api

import (
	"context"
	"errors"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// VoiceRepository stores the Voice menu history; every query is scoped to the owner.
type VoiceRepository struct{ db *gorm.DB }

func NewVoiceRepository(db *gorm.DB) *VoiceRepository { return &VoiceRepository{db: db} }

func (r *VoiceRepository) FindByRequest(ctx context.Context, userID, requestID uuid.UUID) (*models.VoiceGeneration, error) {
	var row models.VoiceGeneration
	if e := r.db.WithContext(ctx).Where("user_id = ? AND request_id = ?", userID, requestID).Take(&row).Error; e != nil {
		return nil, wrapErr("find voice by request", e)
	}
	return &row, nil
}

// Create returns ErrConflict when the owner already used this requestId.
func (r *VoiceRepository) Create(ctx context.Context, row *models.VoiceGeneration) error {
	e := r.db.WithContext(ctx).Create(row).Error
	var pg *pgconn.PgError
	if errors.As(e, &pg) && pg.Code == "23505" || errors.Is(e, gorm.ErrDuplicatedKey) {
		return ErrConflict
	}
	return wrapErr("create voice", e)
}

func (r *VoiceRepository) List(ctx context.Context, userID uuid.UUID, limit, offset int) ([]models.VoiceGeneration, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.VoiceGeneration{}).Where("user_id = ?", userID).Session(&gorm.Session{})
	var total int64
	if e := q.Count(&total).Error; e != nil {
		return nil, 0, wrapErr("count voices", e)
	}
	rows := []models.VoiceGeneration{}
	if total == 0 {
		return rows, 0, nil
	}
	if e := q.Order("created_at DESC, id").Limit(limit).Offset(offset).Find(&rows).Error; e != nil {
		return nil, 0, wrapErr("list voices", e)
	}
	return rows, total, nil
}

func (r *VoiceRepository) Find(ctx context.Context, id, userID uuid.UUID) (*models.VoiceGeneration, error) {
	var row models.VoiceGeneration
	if e := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Take(&row).Error; e != nil {
		return nil, wrapErr("find voice", e)
	}
	return &row, nil
}

// Delete soft-deletes; the stored audio is kept so a mistaken delete can be recovered by an admin.
func (r *VoiceRepository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	return affected("delete voice", r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&models.VoiceGeneration{}))
}

// ── Profiles ("my voices") ────────────────────────────────────────────────────

func (r *VoiceRepository) ListProfiles(ctx context.Context, userID uuid.UUID) ([]models.VoiceProfile, error) {
	rows := []models.VoiceProfile{}
	if e := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC, id").Find(&rows).Error; e != nil {
		return nil, wrapErr("list voice profiles", e)
	}
	return rows, nil
}

func (r *VoiceRepository) CountProfiles(ctx context.Context, userID uuid.UUID) (int64, error) {
	var n int64
	e := r.db.WithContext(ctx).Model(&models.VoiceProfile{}).Where("user_id = ?", userID).Count(&n).Error
	return n, wrapErr("count voice profiles", e)
}

func (r *VoiceRepository) FindProfile(ctx context.Context, id, userID uuid.UUID) (*models.VoiceProfile, error) {
	var row models.VoiceProfile
	if e := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Take(&row).Error; e != nil {
		return nil, wrapErr("find voice profile", e)
	}
	return &row, nil
}

func (r *VoiceRepository) CreateProfile(ctx context.Context, row *models.VoiceProfile) error {
	return wrapErr("create voice profile", r.db.WithContext(ctx).Create(row).Error)
}

// DeleteProfile soft-deletes; generations made with it keep their own voice name.
func (r *VoiceRepository) DeleteProfile(ctx context.Context, id, userID uuid.UUID) error {
	return affected("delete voice profile", r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&models.VoiceProfile{}))
}
