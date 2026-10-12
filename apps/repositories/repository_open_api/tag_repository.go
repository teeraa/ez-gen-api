package repository_open_api

import (
	"context"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"gorm.io/gorm"
)

type TagRepository interface {
	ListByCategory(ctx context.Context, category models.TagCategory) ([]models.DNATag, error)
	ListAll(ctx context.Context) ([]models.DNATag, error)
	FindByID(ctx context.Context, id string) (*models.DNATag, error)
	FindBySlugs(ctx context.Context, slugs []string) ([]models.DNATag, error)
}

type tagRepository struct{ db *gorm.DB }

func NewTagRepository(db *gorm.DB) TagRepository {
	return &tagRepository{db: db}
}

func (r *tagRepository) ListByCategory(ctx context.Context, category models.TagCategory) ([]models.DNATag, error) {
	var tags []models.DNATag
	result := r.db.WithContext(ctx).
		Where("category = ? AND is_active = TRUE", category).
		Order("sort_order ASC, name ASC").
		Find(&tags)
	if result.Error != nil {
		return nil, wrapErr("list tags by category", result.Error)
	}
	return tags, nil
}

func (r *tagRepository) ListAll(ctx context.Context) ([]models.DNATag, error) {
	var tags []models.DNATag
	result := r.db.WithContext(ctx).
		Where("is_active = TRUE").
		Order("category ASC, sort_order ASC, name ASC").
		Find(&tags)
	if result.Error != nil {
		return nil, wrapErr("list all tags", result.Error)
	}
	return tags, nil
}

func (r *tagRepository) FindByID(ctx context.Context, id string) (*models.DNATag, error) {
	var tag models.DNATag
	result := r.db.WithContext(ctx).First(&tag, "id = ? AND is_active = TRUE", id)
	if result.Error != nil {
		return nil, wrapErr("find tag", result.Error)
	}
	return &tag, nil
}

func (r *tagRepository) FindBySlugs(ctx context.Context, slugs []string) ([]models.DNATag, error) {
	if len(slugs) == 0 {
		return nil, nil
	}
	var tags []models.DNATag
	result := r.db.WithContext(ctx).
		Where("slug IN ? AND is_active = TRUE", slugs).
		Find(&tags)
	if result.Error != nil {
		return nil, wrapErr("find tags by slugs", result.Error)
	}
	return tags, nil
}
