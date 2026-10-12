package repository_open_api

import (
	"context"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CharacterProfileRepository interface {
	Create(ctx context.Context, profile *models.CharacterProfile) error
	FindByID(ctx context.Context, id, userID string) (*models.CharacterProfile, error)
	List(ctx context.Context, userID string, page, limit int) ([]models.CharacterProfile, int64, error)
	Update(ctx context.Context, profile *models.CharacterProfile) error
	Delete(ctx context.Context, id, userID string) error
	SetTags(ctx context.Context, profileID uuid.UUID, tagIDs []uuid.UUID) error
}

type characterProfileRepository struct{ db *gorm.DB }

func NewCharacterProfileRepository(db *gorm.DB) CharacterProfileRepository {
	return &characterProfileRepository{db: db}
}

func (r *characterProfileRepository) Create(ctx context.Context, profile *models.CharacterProfile) error {
	if err := r.db.WithContext(ctx).Create(profile).Error; err != nil {
		return wrapErr("create character profile", err)
	}
	return nil
}

func (r *characterProfileRepository) FindByID(ctx context.Context, id, userID string) (*models.CharacterProfile, error) {
	var p models.CharacterProfile
	result := r.db.WithContext(ctx).
		Preload("Tags").
		Preload("Assets").
		Preload("Assets.Upload").
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", id, userID).
		First(&p)
	if result.Error != nil {
		return nil, wrapErr("find character profile", result.Error)
	}
	return &p, nil
}

func (r *characterProfileRepository) List(ctx context.Context, userID string, page, limit int) ([]models.CharacterProfile, int64, error) {
	var profiles []models.CharacterProfile
	var total int64

	base := r.db.WithContext(ctx).Model(&models.CharacterProfile{}).
		Where("user_id = ? AND deleted_at IS NULL", userID)

	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrapErr("count character profiles", err)
	}

	offset := (page - 1) * limit
	result := r.db.WithContext(ctx).
		Preload("Tags").
		Where("user_id = ? AND deleted_at IS NULL", userID).
		Order("created_at DESC").
		Limit(limit).Offset(offset).
		Find(&profiles)
	if result.Error != nil {
		return nil, 0, wrapErr("list character profiles", result.Error)
	}
	return profiles, total, nil
}

func (r *characterProfileRepository) Update(ctx context.Context, profile *models.CharacterProfile) error {
	result := r.db.WithContext(ctx).
		Model(profile).
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", profile.ID, profile.UserID).
		Updates(profile)
	if result.Error != nil {
		return wrapErr("update character profile", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *characterProfileRepository) Delete(ctx context.Context, id, userID string) error {
	result := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", id, userID).
		Delete(&models.CharacterProfile{})
	if result.Error != nil {
		return wrapErr("delete character profile", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *characterProfileRepository) SetTags(ctx context.Context, profileID uuid.UUID, tagIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("character_profile_id = ?", profileID).
			Delete(&models.CharacterProfileTag{}).Error; err != nil {
			return wrapErr("clear character profile tags", err)
		}
		for _, tagID := range tagIDs {
			row := models.CharacterProfileTag{CharacterProfileID: profileID, TagID: tagID}
			if err := tx.Create(&row).Error; err != nil {
				return wrapErr("set character profile tag", err)
			}
		}
		return nil
	})
}
