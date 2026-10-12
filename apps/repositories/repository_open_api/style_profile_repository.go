package repository_open_api

import (
	"context"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type StyleProfileRepository interface {
	Create(ctx context.Context, profile *models.StyleProfile) error
	FindByID(ctx context.Context, id, userID string) (*models.StyleProfile, error)
	List(ctx context.Context, userID string, page, limit int) ([]models.StyleProfile, int64, error)
	Update(ctx context.Context, profile *models.StyleProfile) error
	Delete(ctx context.Context, id, userID string) error
	SetTags(ctx context.Context, profileID uuid.UUID, tagIDs []uuid.UUID) error
}

type styleProfileRepository struct{ db *gorm.DB }

func NewStyleProfileRepository(db *gorm.DB) StyleProfileRepository {
	return &styleProfileRepository{db: db}
}

func (r *styleProfileRepository) Create(ctx context.Context, profile *models.StyleProfile) error {
	if err := r.db.WithContext(ctx).Create(profile).Error; err != nil {
		return wrapErr("create style profile", err)
	}
	return nil
}

func (r *styleProfileRepository) FindByID(ctx context.Context, id, userID string) (*models.StyleProfile, error) {
	var p models.StyleProfile
	result := r.db.WithContext(ctx).
		Preload("Tags").
		Preload("Assets").
		Preload("Assets.Upload").
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", id, userID).
		First(&p)
	if result.Error != nil {
		return nil, wrapErr("find style profile", result.Error)
	}
	return &p, nil
}

func (r *styleProfileRepository) List(ctx context.Context, userID string, page, limit int) ([]models.StyleProfile, int64, error) {
	var profiles []models.StyleProfile
	var total int64

	base := r.db.WithContext(ctx).Model(&models.StyleProfile{}).
		Where("user_id = ? AND deleted_at IS NULL", userID)

	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrapErr("count style profiles", err)
	}

	offset := (page - 1) * limit
	result := r.db.WithContext(ctx).
		Preload("Tags").
		Where("user_id = ? AND deleted_at IS NULL", userID).
		Order("created_at DESC").
		Limit(limit).Offset(offset).
		Find(&profiles)
	if result.Error != nil {
		return nil, 0, wrapErr("list style profiles", result.Error)
	}
	return profiles, total, nil
}

func (r *styleProfileRepository) Update(ctx context.Context, profile *models.StyleProfile) error {
	result := r.db.WithContext(ctx).
		Model(profile).
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", profile.ID, profile.UserID).
		Updates(profile)
	if result.Error != nil {
		return wrapErr("update style profile", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *styleProfileRepository) Delete(ctx context.Context, id, userID string) error {
	result := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", id, userID).
		Delete(&models.StyleProfile{})
	if result.Error != nil {
		return wrapErr("delete style profile", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *styleProfileRepository) SetTags(ctx context.Context, profileID uuid.UUID, tagIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("style_profile_id = ?", profileID).
			Delete(&models.StyleProfileTag{}).Error; err != nil {
			return wrapErr("clear style profile tags", err)
		}
		for _, tagID := range tagIDs {
			row := models.StyleProfileTag{StyleProfileID: profileID, TagID: tagID}
			if err := tx.Create(&row).Error; err != nil {
				return wrapErr("set style profile tag", err)
			}
		}
		return nil
	})
}
