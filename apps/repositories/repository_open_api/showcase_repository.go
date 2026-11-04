package repository_open_api

import (
	"context"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ShowcaseRow is the minimum the landing page needs; it never carries prompts,
// user identifiers or share tokens.
type ShowcaseRow struct {
	ID              uuid.UUID
	Title           string
	Slot            string
	Subtitle        *string
	AspectRatio     string
	DurationSeconds int
	OutputMediaKey  string
	OutputThumbKey  *string
	OutputMediaType *string
}

type ShowcaseRepository interface {
	// ListPublic returns active items whose job succeeded, is not deleted, has a
	// stored output and still has an active share link chosen by its owner.
	ListPublic(ctx context.Context, limit int) ([]ShowcaseRow, error)
}

type showcaseRepository struct{ db *gorm.DB }

func NewShowcaseRepository(db *gorm.DB) ShowcaseRepository {
	return &showcaseRepository{db: db}
}

func (r *showcaseRepository) ListPublic(ctx context.Context, limit int) ([]ShowcaseRow, error) {
	var rows []ShowcaseRow
	err := r.db.WithContext(ctx).
		Table("showcase_items AS si").
		Select(`si.id, si.title, si.slot, si.subtitle, j.aspect_ratio, j.duration_seconds,
			j.output_media_key, j.output_thumb_key, j.output_media_type`).
		Joins("JOIN video_generation_jobs j ON j.id = si.job_id").
		Where("si.is_active").
		Where("j.deleted_at IS NULL AND j.status = ? AND j.output_media_key IS NOT NULL", models.JobStatusSucceeded).
		Where(`EXISTS (SELECT 1 FROM video_shares vs
			WHERE vs.job_id = j.id AND vs.user_id = j.user_id AND vs.revoked_at IS NULL)`).
		Order("si.slot ASC, si.sort_order ASC, si.created_at DESC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, wrapErr("list showcase", err)
	}
	return rows, nil
}
