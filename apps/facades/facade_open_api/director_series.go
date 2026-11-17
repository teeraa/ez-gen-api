package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/google/uuid"
)

const (
	directorSeriesNameMax   = 120
	directorEpisodeTitleMax = 120
	// Project names are limited to 80 runes by settings(); generated episode names must fit it.
	directorProjectNameMax = 80
	// Series context is descriptive only; these caps keep it a small fraction of the planner input.
	directorSeriesContextEpisodes   = 5
	directorSeriesContextLoglineMax = 300
)

// directorSeriesBrief is the planner's read-only view of the series an episode belongs to.
type directorSeriesBrief struct {
	Note            string                 `json:"note"`
	Name            string                 `json:"name"`
	EpisodeNumber   int                    `json:"episodeNumber"`
	EpisodeTitle    string                 `json:"episodeTitle,omitempty"`
	EarlierEpisodes []directorEpisodeBrief `json:"earlierEpisodes"`
}
type directorEpisodeBrief struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Logline string `json:"logline,omitempty"`
}

// Series modes: the menu a series is listed under. Episodes, jobs and points are shared.
const (
	DirectorModeDirector = "director"
	DirectorModeFilm     = "film"
)

func directorValidMode(m string) bool { return m == DirectorModeDirector || m == DirectorModeFilm }

const directorSeriesNote = "This project is one episode of a series. Write only this episode's story. Earlier episodes are background for continuity of world and characters; do not retell them."

func directorEpisodeTitle(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func directorClipRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:max]))
}

// directorEpisodeName keeps the episode suffix intact and shortens the series name instead.
func directorEpisodeName(series string, number int, title *string) string {
	if title != nil {
		return directorClipRunes(*title, directorProjectNameMax)
	}
	suffix := " ตอนที่ " + strconv.Itoa(number)
	return directorClipRunes(strings.TrimSpace(series), directorProjectNameMax-utf8.RuneCountInString(suffix)) + suffix
}

func directorSeriesCast(s *models.DirectorSeries) []string {
	cast := []string{}
	if len(s.CharacterAssetIDs) > 0 {
		_ = json.Unmarshal(s.CharacterAssetIDs, &cast)
	}
	if cast == nil {
		cast = []string{}
	}
	return cast
}

func directorValidCastAsset(a *models.LibraryAsset) bool {
	return a != nil && a.AssetType == "character" && a.StorageKey != nil
}

func directorValidateCast(ids []string, loadAsset func(string) (*models.LibraryAsset, error)) error {
	if len(ids) > 4 {
		return directorInvalid("characterAssetIds")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if _, e := uuid.Parse(id); e != nil || seen[id] {
			return directorInvalid("characterAssetIds")
		}
		seen[id] = true
		a, e := loadAsset(id)
		if e != nil || !directorValidCastAsset(a) {
			return directorInvalid("characterAssetIds")
		}
	}
	return nil
}

func (f *DirectorFacade) validateSeries(s *models.DirectorSeries) error {
	if !directorBound(s.Name, 1, directorSeriesNameMax) {
		return directorInvalid("name")
	}
	// The project name rule is checked separately above; a placeholder isolates the enum checks.
	return f.settings(&dto.DirectorProject{Name: "series", AspectRatio: s.AspectRatio, Style: s.Style, Resolution: s.Resolution})
}

func directorSeriesBase(s *models.DirectorSeries) dto.DirectorSeries {
	return dto.DirectorSeries{ID: s.ID.String(), Revision: s.Revision, Name: s.Name, Mode: s.Mode, AspectRatio: s.AspectRatio, Style: s.Style, Resolution: s.Resolution, CharacterAssetIDs: directorSeriesCast(s), CreatedAt: s.CreatedAt.UTC(), UpdatedAt: s.UpdatedAt.UTC()}
}

func (f *DirectorFacade) signCover(c context.Context, key string) (*string, *time.Time, error) {
	if key == "" {
		return nil, nil, nil
	}
	u, t, e := f.storage.SignedURL(c, key, time.Hour)
	if e != nil {
		return nil, nil, e
	}
	return &u, &t, nil
}

// directorCoverKey follows present(): a succeeded cover wins, otherwise the first succeeded shot.
func directorCoverKey(d *directorDocument) string {
	p := &d.Project
	if p.Cover != nil && p.Cover.Status == "succeeded" {
		return d.Keys[p.Cover.ID]
	}
	for _, sc := range p.Scenes {
		for _, sh := range sc.Shots {
			if sh.Image.Status == "succeeded" {
				return d.Keys[sh.ID]
			}
		}
	}
	return ""
}

// summarize builds a list row signing only the cover, unlike present() which signs every shot.
func (f *DirectorFacade) summarize(c context.Context, m *models.DirectorProject) (dto.DirectorSummary, error) {
	d, e := directorDecode(m)
	if e != nil {
		return dto.DirectorSummary{}, e
	}
	p := d.Project
	directorEpisodeFields(m, &p)
	p.CoverImageURL, p.CoverImageExpiresAt, e = f.signCover(c, directorCoverKey(&d))
	return directorSummary(p), e
}

// seriesStats fills episodeCount and the cover from SQL aggregates without decoding episodes.
func (f *DirectorFacade) seriesStats(c context.Context, user string, rows []models.DirectorSeries) ([]dto.DirectorSeries, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, s := range rows {
		ids = append(ids, s.ID)
	}
	stats, e := f.store.SeriesStats(c, user, ids)
	if e != nil {
		return nil, e
	}
	out := make([]dto.DirectorSeries, 0, len(rows))
	for i := range rows {
		s := directorSeriesBase(&rows[i])
		st := stats[rows[i].ID]
		s.EpisodeCount = st.EpisodeCount
		if s.CoverImageURL, s.CoverImageExpiresAt, e = f.signCover(c, st.CoverKey); e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, nil
}

func (f *DirectorFacade) oneSeries(c context.Context, user string, s *models.DirectorSeries) (dto.DirectorSeries, error) {
	out, e := f.seriesStats(c, user, []models.DirectorSeries{*s})
	if e != nil {
		return dto.DirectorSeries{}, e
	}
	return out[0], nil
}

func (f *DirectorFacade) CreateSeries(c context.Context, user string, in req.DirectorSeriesRequest) (dto.DirectorSeries, error) {
	if _, e := uuid.Parse(in.IdempotencyKey); e != nil {
		return dto.DirectorSeries{}, directorInvalid("idempotencyKey")
	}
	if in.Name == nil || in.AspectRatio == nil || in.Style == nil || in.Resolution == nil {
		return dto.DirectorSeries{}, directorInvalid("request")
	}
	owner, e := uuid.Parse(user)
	if e != nil {
		return dto.DirectorSeries{}, repo.ErrNotFound
	}
	mode := DirectorModeDirector
	if in.Mode != nil {
		mode = *in.Mode
	}
	if !directorValidMode(mode) {
		return dto.DirectorSeries{}, directorInvalid("mode")
	}
	cast := []string{}
	if in.CharacterAssetIDs != nil {
		cast = *in.CharacterAssetIDs
	}
	raw, e := json.Marshal(cast)
	if e != nil {
		return dto.DirectorSeries{}, e
	}
	now := time.Now().UTC()
	s := models.DirectorSeries{Base: models.Base{ID: uuid.New(), CreatedAt: now, UpdatedAt: now}, UserID: owner, Name: *in.Name, Mode: mode, AspectRatio: *in.AspectRatio, Style: *in.Style, Resolution: *in.Resolution, CharacterAssetIDs: raw, Revision: 1}
	if e = f.validateSeries(&s); e != nil {
		return dto.DirectorSeries{}, e
	}
	row, e := f.store.CreateSeries(c, &s, in.IdempotencyKey, storyHash(struct {
		Action string
		Input  req.DirectorSeriesRequest
	}{"series-create", in}), func(_ *models.DirectorSeries, loadAsset repo.DirectorAssetLoader) error {
		return directorValidateCast(cast, loadAsset)
	})
	if e != nil {
		return dto.DirectorSeries{}, e
	}
	return f.oneSeries(c, user, row)
}

func (f *DirectorFacade) ListSeries(c context.Context, user, mode string, page, limit int) (dto.DirectorSeriesList, error) {
	out := dto.DirectorSeriesList{Items: []dto.DirectorSeries{}, Page: page, Limit: limit}
	if page < 1 || limit < 1 || limit > 50 {
		return out, directorInvalid("pagination")
	}
	if !directorValidMode(mode) {
		return out, directorInvalid("mode")
	}
	rows, n, e := f.store.ListSeries(c, user, mode, page, limit)
	out.Total = n
	if e != nil {
		return out, e
	}
	items, e := f.seriesStats(c, user, rows)
	if e != nil {
		return out, e
	}
	out.Items = items
	return out, nil
}

func (f *DirectorFacade) GetSeries(c context.Context, id, user string) (dto.DirectorSeriesDetail, error) {
	if _, e := uuid.Parse(id); e != nil {
		return dto.DirectorSeriesDetail{}, repo.ErrNotFound
	}
	s, e := f.store.GetSeries(c, id, user)
	if e != nil {
		return dto.DirectorSeriesDetail{}, e
	}
	episodes, e := f.store.SeriesEpisodes(c, user, []uuid.UUID{s.ID})
	if e != nil {
		return dto.DirectorSeriesDetail{}, e
	}
	out := dto.DirectorSeriesDetail{DirectorSeries: directorSeriesBase(s), Episodes: make([]dto.DirectorSummary, 0, len(episodes))}
	stats, e := f.store.SeriesStats(c, user, []uuid.UUID{s.ID})
	if e != nil {
		return out, e
	}
	st := stats[s.ID]
	for i := range episodes {
		sum, e := f.summarize(c, &episodes[i])
		if e != nil {
			return out, e
		}
		// Film episodes have no storyboard images: use the first clip frame, else a series element.
		if sum.CoverImageURL == nil {
			key := st.EpisodeCovers[episodes[i].ID]
			if key == "" {
				key = st.ElementCover
			}
			if sum.CoverImageURL, sum.CoverImageExpiresAt, e = f.signCover(c, key); e != nil {
				return out, e
			}
		}
		if out.CoverImageURL == nil && sum.CoverImageURL != nil {
			out.CoverImageURL, out.CoverImageExpiresAt = sum.CoverImageURL, sum.CoverImageExpiresAt
		}
		out.Episodes = append(out.Episodes, sum)
	}
	out.EpisodeCount = len(out.Episodes)
	return out, nil
}

// UpdateSeries changes the defaults used for future episodes; existing episodes keep their settings.
func (f *DirectorFacade) UpdateSeries(c context.Context, id, user string, in req.DirectorSeriesRequest) (dto.DirectorSeries, error) {
	if _, e := uuid.Parse(id); e != nil {
		return dto.DirectorSeries{}, repo.ErrNotFound
	}
	if in.Revision == nil {
		return dto.DirectorSeries{}, directorInvalid("revision")
	}
	row, e := f.store.ChangeSeries(c, id, user, func(s *models.DirectorSeries, loadAsset repo.DirectorAssetLoader) error {
		if *in.Revision != s.Revision {
			return &DirectorError{Status: 409, Code: "REVISION_CONFLICT", Message: "มีการแก้ไขจากอีกแท็บ กรุณาโหลดข้อมูลล่าสุด", CurrentRevision: directorPointer(s.Revision)}
		}
		// The owning menu is fixed at creation; resending the same value is harmless.
		if in.Mode != nil && *in.Mode != s.Mode {
			return directorInvalid("mode")
		}
		changed := false
		if in.Name != nil {
			s.Name, changed = *in.Name, true
		}
		if in.AspectRatio != nil {
			s.AspectRatio, changed = *in.AspectRatio, true
		}
		if in.Style != nil {
			s.Style, changed = *in.Style, true
		}
		if in.Resolution != nil {
			s.Resolution, changed = *in.Resolution, true
		}
		if in.CharacterAssetIDs != nil {
			if e := directorValidateCast(*in.CharacterAssetIDs, loadAsset); e != nil {
				return e
			}
			raw, e := json.Marshal(*in.CharacterAssetIDs)
			if e != nil {
				return e
			}
			s.CharacterAssetIDs, changed = raw, true
		}
		if e := f.validateSeries(s); e != nil {
			return e
		}
		if changed {
			s.Revision++
		}
		return nil
	})
	if e != nil {
		return dto.DirectorSeries{}, e
	}
	return f.oneSeries(c, user, row)
}

func (f *DirectorFacade) DeleteSeries(c context.Context, id, user string) error {
	if _, e := uuid.Parse(id); e != nil {
		return repo.ErrNotFound
	}
	e := f.store.DeleteSeries(c, id, user)
	if errors.Is(e, repo.ErrDirectorSeriesBusy) {
		return &DirectorError{Status: 409, Code: "SERIES_BUSY", Message: "มีตอนที่กำลังสร้างงานอยู่ กรุณารอให้เสร็จก่อนลบโปรเจกต์"}
	}
	return e
}

// CreateEpisode starts a new episode at the idea step with the series defaults as initial values.
func (f *DirectorFacade) CreateEpisode(c context.Context, seriesID, user string, in req.DirectorEpisodeRequest) (dto.DirectorProject, error) {
	if _, e := uuid.Parse(seriesID); e != nil {
		return dto.DirectorProject{}, repo.ErrNotFound
	}
	if _, e := uuid.Parse(in.IdempotencyKey); e != nil {
		return dto.DirectorProject{}, directorInvalid("idempotencyKey")
	}
	var title *string
	if in.Title != nil {
		if !directorBound(*in.Title, 0, directorEpisodeTitleMax) {
			return dto.DirectorProject{}, directorInvalid("title")
		}
		title = directorEpisodeTitle(*in.Title)
	}
	row, e := f.store.CreateEpisode(c, seriesID, user, in.IdempotencyKey, storyHash(struct {
		Action, Series string
		Input          req.DirectorEpisodeRequest
	}{"episode-create", seriesID, in}), func(s *models.DirectorSeries, number int, loadAsset repo.DirectorAssetLoader) (*models.DirectorProject, error) {
		// Assets deleted since the series was saved are dropped rather than blocking the episode.
		cast := []string{}
		for _, id := range directorSeriesCast(s) {
			if a, e := loadAsset(id); e == nil && directorValidCastAsset(a) {
				cast = append(cast, id)
			}
		}
		m, _, e := f.newProjectRow(user, directorEpisodeName(s.Name, number, title), s.AspectRatio, s.Style, s.Resolution, cast)
		if e != nil {
			return nil, e
		}
		m.EpisodeTitle = title
		return m, nil
	})
	if errors.Is(e, repo.ErrDirectorEpisodeFull) {
		return dto.DirectorProject{}, &DirectorError{Status: 409, Code: "EPISODE_LIMIT", Message: "จำนวนตอนในโปรเจกต์นี้ถึงขีดจำกัดแล้ว"}
	}
	if e != nil {
		return dto.DirectorProject{}, e
	}
	return f.present(c, row)
}

// seriesBrief is best effort: a failed lookup only drops descriptive context, never the paid story job.
func (f *DirectorFacade) seriesBrief(c context.Context, p *models.DirectorProject) *directorSeriesBrief {
	ctx, e := f.store.SeriesContext(c, p.SeriesID.String(), p.UserID.String(), *p.EpisodeNumber)
	if e != nil {
		log.Printf("director: project=%s series context unavailable: %v", p.ID, directorCause(e))
		return nil
	}
	out := &directorSeriesBrief{Note: directorSeriesNote, Name: directorClipRunes(ctx.SeriesName, directorSeriesNameMax), EpisodeNumber: *p.EpisodeNumber, EarlierEpisodes: []directorEpisodeBrief{}}
	if p.EpisodeTitle != nil {
		out.EpisodeTitle = directorClipRunes(*p.EpisodeTitle, directorEpisodeTitleMax)
	}
	earlier := ctx.Earlier
	if len(earlier) > directorSeriesContextEpisodes {
		earlier = earlier[len(earlier)-directorSeriesContextEpisodes:]
	}
	for _, ep := range earlier {
		out.EarlierEpisodes = append(out.EarlierEpisodes, directorEpisodeBrief{Number: ep.Number, Title: directorClipRunes(ep.Title, directorEpisodeTitleMax), Logline: directorClipRunes(ep.Logline, directorSeriesContextLoglineMax)})
	}
	return out
}
