package repository_open_api

import (
	"context"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SceneRepository interface {
	Create(ctx context.Context, scene *models.Scene) error
	FindByID(ctx context.Context, id, userID string) (*models.Scene, error)
	List(ctx context.Context, userID string, page, limit int) ([]models.Scene, int64, error)
	Update(ctx context.Context, scene *models.Scene) error
	Delete(ctx context.Context, id, userID string) error
	SetTags(ctx context.Context, sceneID uuid.UUID, tagIDs []uuid.UUID) error
	SetSelectedJob(ctx context.Context, sceneID uuid.UUID, jobID uuid.UUID, userID string) error
	SetCharacters(ctx context.Context, sceneID uuid.UUID, characterIDs []uuid.UUID) error
}

type sceneRepository struct{ db *gorm.DB }

func NewSceneRepository(db *gorm.DB) SceneRepository {
	return &sceneRepository{db: db}
}

func (r *sceneRepository) Create(ctx context.Context, scene *models.Scene) error {
	if err := r.db.WithContext(ctx).Create(scene).Error; err != nil {
		return wrapErr("create scene", err)
	}
	return nil
}

func (r *sceneRepository) FindByID(ctx context.Context, id, userID string) (*models.Scene, error) {
	var s models.Scene
	result := r.db.WithContext(ctx).
		Preload("Tags").
		Preload("Assets").
		Preload("Assets.Upload").
		Preload("CharacterProfile").
		Preload("CharacterProfile.Tags").
		Preload("Characters", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order ASC") }).
		Preload("Characters.CharacterProfile").
		Preload("StyleProfile").
		Preload("StyleProfile.Tags").
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", id, userID).
		First(&s)
	if result.Error != nil {
		return nil, wrapErr("find scene", result.Error)
	}
	return &s, nil
}

func (r *sceneRepository) List(ctx context.Context, userID string, page, limit int) ([]models.Scene, int64, error) {
	var scenes []models.Scene
	var total int64

	base := r.db.WithContext(ctx).Model(&models.Scene{}).
		Where("user_id = ? AND deleted_at IS NULL", userID)

	if err := base.Count(&total).Error; err != nil {
		return nil, 0, wrapErr("count scenes", err)
	}

	offset := (page - 1) * limit
	result := r.db.WithContext(ctx).
		Preload("Tags").
		Preload("Characters", func(db *gorm.DB) *gorm.DB { return db.Order("sort_order ASC") }).
		Preload("Characters.CharacterProfile").
		Where("user_id = ? AND deleted_at IS NULL", userID).
		Order("scene_order ASC, created_at DESC").
		Limit(limit).Offset(offset).
		Find(&scenes)
	if result.Error != nil {
		return nil, 0, wrapErr("list scenes", result.Error)
	}
	return scenes, total, nil
}

func (r *sceneRepository) Update(ctx context.Context, scene *models.Scene) error {
	// Associations are written by SetTags/SetCharacters; saving the preloaded copies
	// here would race those replacements.
	result := r.db.WithContext(ctx).
		Model(scene).
		Omit(clause.Associations).
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", scene.ID, scene.UserID).
		Updates(scene)
	if result.Error != nil {
		return wrapErr("update scene", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *sceneRepository) Delete(ctx context.Context, id, userID string) error {
	result := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", id, userID).
		Delete(&models.Scene{})
	if result.Error != nil {
		return wrapErr("delete scene", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *sceneRepository) SetTags(ctx context.Context, sceneID uuid.UUID, tagIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("scene_id = ?", sceneID).
			Delete(&models.SceneTag{}).Error; err != nil {
			return wrapErr("clear scene tags", err)
		}
		for _, tagID := range tagIDs {
			row := models.SceneTag{SceneID: sceneID, TagID: tagID}
			if err := tx.Create(&row).Error; err != nil {
				return wrapErr("set scene tag", err)
			}
		}
		return nil
	})
}

func (r *sceneRepository) SetSelectedJob(ctx context.Context, sceneID uuid.UUID, jobID uuid.UUID, userID string) error {
	result := r.db.WithContext(ctx).
		Model(&models.Scene{}).
		Where("id = ? AND user_id = ? AND deleted_at IS NULL", sceneID, userID).
		Update("selected_job_id", jobID)
	if result.Error != nil {
		return wrapErr("set selected job", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetCharacters replaces a scene's characters in order and mirrors the first one into
// scenes.character_profile_id, in one transaction so the two never disagree.
// Callers must have checked that the profiles belong to the scene's owner.
func (r *sceneRepository) SetCharacters(ctx context.Context, sceneID uuid.UUID, characterIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("scene_id = ?", sceneID).Delete(&models.SceneCharacter{}).Error; err != nil {
			return wrapErr("clear scene characters", err)
		}
		for i, id := range characterIDs {
			row := models.SceneCharacter{SceneID: sceneID, CharacterProfileID: id, SortOrder: i}
			if err := tx.Omit(clause.Associations).Create(&row).Error; err != nil {
				return wrapErr("set scene character", err)
			}
		}
		var first *uuid.UUID
		if len(characterIDs) > 0 {
			first = &characterIDs[0]
		}
		if err := tx.Model(&models.Scene{}).Where("id = ?", sceneID).
			Update("character_profile_id", first).Error; err != nil {
			return wrapErr("sync scene primary character", err)
		}
		return nil
	})
}
