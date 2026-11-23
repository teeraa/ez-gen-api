package repository_open_api

import (
	"context"
	"errors"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrDirectorSeriesBusy  = errors.New("director series has active jobs")
	ErrDirectorEpisodeFull = errors.New("director series episode limit reached")
)

// directorEpisodeMax mirrors the episode_number CHECK constraint in migration 030.
const directorEpisodeMax = 999

var directorActiveJobStatuses = []string{"queued", "submitting", "waiting"}

type DirectorEpisodeBrief struct {
	Number  int
	Title   string
	Logline string
}

// DirectorSeriesStats summarises a series' live episodes without loading their documents.
// CoverKey is the storage key of the lowest-numbered episode that has a cover image.
type DirectorSeriesStats struct {
	EpisodeCount int
	CoverKey     string
	// Film (ภาพยนตร์) episodes have no storyboard images: EpisodeCovers holds each episode's first
	// finished clip frame and ElementCover a reference image of the series (a location first).
	EpisodeCovers map[uuid.UUID]string
	ElementCover  string
}

type DirectorSeriesContext struct {
	SeriesName string
	Earlier    []DirectorEpisodeBrief
}

type DirectorAssetLoader = func(string) (*models.LibraryAsset, error)

func directorAssetLoader(tx *gorm.DB, user string) DirectorAssetLoader {
	return func(assetID string) (*models.LibraryAsset, error) {
		var asset models.LibraryAsset
		err := tx.Where("id=? AND user_id=?", assetID, user).First(&asset).Error
		return &asset, wrapErr("director owned asset", err)
	}
}

func (r *DirectorRepository) CreateSeries(c context.Context, s *models.DirectorSeries, key, hash string, validate func(*models.DirectorSeries, DirectorAssetLoader) error) (*models.DirectorSeries, error) {
	user := s.UserID.String()
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := directorLock(tx, user); e != nil {
			return e
		}
		var receipt models.DirectorSeriesReceipt
		e := tx.Where("user_id=? AND key=?", user, key).First(&receipt).Error
		if e == nil {
			if receipt.Hash != hash {
				return ErrDirectorIdempotency
			}
			// Deleted series cannot be resurrected by replay.
			var live models.DirectorSeries
			if e = tx.Where("id=? AND user_id=?", receipt.SeriesID, user).First(&live).Error; e != nil {
				return e
			}
			*s = live
			return nil
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if e = validate(s, directorAssetLoader(tx, user)); e != nil {
			return e
		}
		if e = tx.Create(s).Error; e != nil {
			return e
		}
		return tx.Create(&models.DirectorSeriesReceipt{UserID: s.UserID, Key: key, Hash: hash, SeriesID: s.ID, CreatedAt: time.Now()}).Error
	})
	return s, wrapErr("director series create", e)
}

func (r *DirectorRepository) GetSeries(c context.Context, id, user string) (*models.DirectorSeries, error) {
	var s models.DirectorSeries
	e := r.db.WithContext(c).Where("id=? AND user_id=?", id, user).First(&s).Error
	return &s, wrapErr("director series get", e)
}

func (r *DirectorRepository) ListSeries(c context.Context, user, mode string, page, limit int) ([]models.DirectorSeries, int64, error) {
	q := r.db.WithContext(c).Model(&models.DirectorSeries{}).Where("user_id=? AND mode=?", user, mode)
	var n int64
	if e := q.Count(&n).Error; e != nil {
		return nil, 0, wrapErr("director series count", e)
	}
	items := []models.DirectorSeries{}
	e := q.Order("updated_at DESC,id DESC").Offset((page - 1) * limit).Limit(limit).Find(&items).Error
	return items, n, wrapErr("director series list", e)
}

// SeriesEpisodes returns the live episodes of the given series, ordered by series then episode number.
func (r *DirectorRepository) SeriesEpisodes(c context.Context, user string, seriesIDs []uuid.UUID) ([]models.DirectorProject, error) {
	items := []models.DirectorProject{}
	if len(seriesIDs) == 0 {
		return items, nil
	}
	e := r.db.WithContext(c).Where("user_id=? AND series_id IN ?", user, seriesIDs).Order("series_id,episode_number,id").Find(&items).Error
	return items, wrapErr("director series episodes", e)
}

// directorEpisodeCoverKeySQL mirrors present(): a succeeded cover wins, otherwise the first
// succeeded shot image in scene/shot order.
const directorEpisodeCoverKeySQL = `CASE WHEN p.document#>>'{project,cover,status}'='succeeded'
 THEN p.document->'keys'->>(p.document#>>'{project,cover,id}')
 ELSE (SELECT p.document->'keys'->>(shot.v->>'id')
  FROM jsonb_array_elements(coalesce(p.document#>'{project,scenes}','[]'::jsonb)) WITH ORDINALITY AS scene(v,i)
  CROSS JOIN LATERAL jsonb_array_elements(coalesce(scene.v->'shots','[]'::jsonb)) WITH ORDINALITY AS shot(v,j)
  WHERE shot.v#>>'{image,status}'='succeeded' ORDER BY scene.i, shot.j LIMIT 1) END`

// SeriesStats counts live episodes and picks the cover key in SQL, so listing never decodes documents.
func (r *DirectorRepository) SeriesStats(c context.Context, user string, seriesIDs []uuid.UUID) (map[uuid.UUID]DirectorSeriesStats, error) {
	out := map[uuid.UUID]DirectorSeriesStats{}
	if len(seriesIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		SeriesID     uuid.UUID
		EpisodeCount int
		CoverKey     *string
	}
	e := r.db.WithContext(c).Raw(`WITH e AS (SELECT p.series_id, p.episode_number, `+directorEpisodeCoverKeySQL+` AS cover_key
 FROM director_projects p WHERE p.user_id=? AND p.series_id IN ? AND p.deleted_at IS NULL)
SELECT series_id, count(*) AS episode_count,
 (array_agg(cover_key ORDER BY episode_number) FILTER (WHERE coalesce(cover_key,'')<>''))[1] AS cover_key
FROM e GROUP BY series_id`, user, seriesIDs).Scan(&rows).Error
	if e != nil {
		return nil, wrapErr("director series stats", e)
	}
	for _, row := range rows {
		st := DirectorSeriesStats{EpisodeCount: row.EpisodeCount}
		if row.CoverKey != nil {
			st.CoverKey = *row.CoverKey
		}
		out[row.SeriesID] = st
	}
	return out, r.filmCovers(c, user, seriesIDs, out)
}

// filmCovers fills cover keys for film series from their clips and element images.
func (r *DirectorRepository) filmCovers(c context.Context, user string, seriesIDs []uuid.UUID, out map[uuid.UUID]DirectorSeriesStats) error {
	var frames []struct {
		SeriesID  uuid.UUID
		ProjectID uuid.UUID
		FrameKey  string
	}
	e := r.db.WithContext(c).Raw(`SELECT p.series_id, p.id AS project_id,
 (SELECT shot.v#>>'{video,frameKey}' FROM jsonb_array_elements(coalesce(fe.document->'shots','[]'::jsonb)) WITH ORDINALITY AS shot(v,i)
  WHERE shot.v#>>'{video,status}'='succeeded' AND coalesce(shot.v#>>'{video,frameKey}','')<>'' ORDER BY shot.i LIMIT 1) AS frame_key
FROM film_episodes fe JOIN director_projects p ON p.id=fe.project_id
WHERE p.user_id=? AND p.series_id IN ? AND p.deleted_at IS NULL
ORDER BY p.series_id, p.episode_number`, user, seriesIDs).Scan(&frames).Error
	if e != nil {
		return wrapErr("film episode covers", e)
	}
	var elements []struct {
		SeriesID uuid.UUID
		ImageKey string
	}
	e = r.db.WithContext(c).Raw(`SELECT DISTINCT ON (series_id) series_id, image_key FROM film_elements
WHERE user_id=? AND series_id IN ? AND deleted_at IS NULL AND image_status='succeeded' AND coalesce(image_key,'')<>''
ORDER BY series_id, (kind='location') DESC, created_at`, user, seriesIDs).Scan(&elements).Error
	if e != nil {
		return wrapErr("film element covers", e)
	}
	for _, f := range frames {
		st := out[f.SeriesID]
		if st.EpisodeCovers == nil {
			st.EpisodeCovers = map[uuid.UUID]string{}
		}
		if f.FrameKey != "" {
			st.EpisodeCovers[f.ProjectID] = f.FrameKey
			if st.CoverKey == "" {
				st.CoverKey = f.FrameKey
			}
		}
		out[f.SeriesID] = st
	}
	for _, el := range elements {
		st := out[el.SeriesID]
		st.ElementCover = el.ImageKey
		if st.CoverKey == "" {
			st.CoverKey = el.ImageKey
		}
		out[el.SeriesID] = st
	}
	return nil
}

func (r *DirectorRepository) ChangeSeries(c context.Context, id, user string, fn func(*models.DirectorSeries, DirectorAssetLoader) error) (*models.DirectorSeries, error) {
	var s models.DirectorSeries
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := directorLock(tx, user); e != nil {
			return e
		}
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", id, user).First(&s).Error; e != nil {
			return e
		}
		revision := s.Revision
		if e := fn(&s, directorAssetLoader(tx, user)); e != nil {
			return e
		}
		// Every real change bumps the revision, so an unchanged revision is a no-op PATCH.
		if s.Revision == revision {
			return nil
		}
		s.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond) // Postgres timestamps keep microseconds; match what a later read returns.
		return tx.Model(&s).Updates(map[string]any{"name": s.Name, "aspect_ratio": s.AspectRatio, "style": s.Style, "resolution": s.Resolution, "character_asset_ids": s.CharacterAssetIDs, "revision": s.Revision, "updated_at": s.UpdatedAt}).Error
	})
	return &s, wrapErr("director series change", e)
}

// DeleteSeries soft-deletes a series and its episodes unless any episode still has a job that may
// charge, submit or complete; those must finish first so no work lands on a deleted episode.
func (r *DirectorRepository) DeleteSeries(c context.Context, id, user string) error {
	return wrapErr("director series delete", r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := directorLock(tx, user); e != nil {
			return e
		}
		var s models.DirectorSeries
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", id, user).First(&s).Error; e != nil {
			return e
		}
		// Episode rows are locked first, matching Claim, so a concurrent claim is either visible or blocked.
		var episodes []models.DirectorProject
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("series_id=? AND user_id=?", s.ID, user).Find(&episodes).Error; e != nil {
			return e
		}
		if len(episodes) > 0 {
			ids := make([]uuid.UUID, 0, len(episodes))
			for _, p := range episodes {
				ids = append(ids, p.ID)
			}
			var busy int64
			if e := tx.Model(&models.DirectorJob{}).Where("user_id=? AND project_id IN ? AND status IN ?", user, ids, directorActiveJobStatuses).Count(&busy).Error; e != nil {
				return e
			}
			if busy > 0 {
				return ErrDirectorSeriesBusy
			}
		}
		now := time.Now()
		if e := tx.Model(&models.DirectorProject{}).Where("series_id=? AND user_id=?", s.ID, user).Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error; e != nil {
			return e
		}
		return tx.Model(&s).Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error
	}))
}

// CreateEpisode numbers the new episode max+1 while holding the owner's Director lock and the
// series row lock, so concurrent creates cannot pick the same number.
func (r *DirectorRepository) CreateEpisode(c context.Context, seriesID, user, key, hash string, build func(*models.DirectorSeries, int, DirectorAssetLoader) (*models.DirectorProject, error)) (*models.DirectorProject, error) {
	var out *models.DirectorProject
	e := r.db.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if e := directorLock(tx, user); e != nil {
			return e
		}
		var s models.DirectorSeries
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND user_id=?", seriesID, user).First(&s).Error; e != nil {
			return e
		}
		old, e := directorReplay(tx, user, key, hash)
		if e != nil {
			return e
		}
		if old != nil {
			if old.SeriesID == nil || *old.SeriesID != s.ID {
				return ErrDirectorIdempotency
			}
			out = old
			return nil
		}
		var last *int
		if e = tx.Model(&models.DirectorProject{}).Where("series_id=? AND user_id=?", s.ID, user).Select("max(episode_number)").Scan(&last).Error; e != nil {
			return e
		}
		next := 1
		if last != nil {
			next = *last + 1
		}
		if next > directorEpisodeMax {
			return ErrDirectorEpisodeFull
		}
		p, e := build(&s, next, directorAssetLoader(tx, user))
		if e != nil {
			return e
		}
		p.SeriesID = &s.ID
		p.EpisodeNumber = &next
		if e = tx.Create(p).Error; e != nil {
			return e
		}
		if e = tx.Model(&s).Update("updated_at", time.Now()).Error; e != nil {
			return e
		}
		out = p
		return tx.Create(&models.DirectorReceipt{UserID: p.UserID, Key: key, Hash: hash, ProjectID: p.ID, Response: p.Document, CreatedAt: time.Now()}).Error
	})
	return out, wrapErr("director episode create", e)
}

// SeriesContext describes the series and up to five earlier live episodes for story planning.
func (r *DirectorRepository) SeriesContext(c context.Context, seriesID, user string, before int) (*DirectorSeriesContext, error) {
	var s models.DirectorSeries
	if e := r.db.WithContext(c).Select("id", "name").Where("id=? AND user_id=?", seriesID, user).First(&s).Error; e != nil {
		return nil, wrapErr("director series context", e)
	}
	var rows []struct {
		EpisodeNumber int
		EpisodeTitle  *string
		Name          string
		Logline       *string
	}
	e := r.db.WithContext(c).Model(&models.DirectorProject{}).
		Select("episode_number, episode_title, name, document#>>'{project,story,logline}' AS logline").
		Where("series_id=? AND user_id=? AND episode_number < ?", seriesID, user, before).
		Order("episode_number DESC").Limit(5).Scan(&rows).Error
	if e != nil {
		return nil, wrapErr("director series context episodes", e)
	}
	out := &DirectorSeriesContext{SeriesName: s.Name, Earlier: []DirectorEpisodeBrief{}}
	for i := len(rows) - 1; i >= 0; i-- {
		b := DirectorEpisodeBrief{Number: rows[i].EpisodeNumber, Title: rows[i].Name}
		if rows[i].EpisodeTitle != nil && *rows[i].EpisodeTitle != "" {
			b.Title = *rows[i].EpisodeTitle
		}
		if rows[i].Logline != nil {
			b.Logline = *rows[i].Logline
		}
		out.Earlier = append(out.Earlier, b)
	}
	return out, nil
}
