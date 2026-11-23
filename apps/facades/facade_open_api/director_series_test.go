package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/google/uuid"
)

type seriesPromptStore struct {
	directorStore
	row         models.DirectorProject
	job         models.DirectorJob
	context     *repo.DirectorSeriesContext
	contextErr  error
	contextArgs []any
}

func (s *seriesPromptStore) Claim(context.Context) (*models.DirectorJob, error) {
	j := s.job
	return &j, nil
}
func (s *seriesPromptStore) Get(context.Context, string, string) (*models.DirectorProject, error) {
	return &s.row, nil
}
func (s *seriesPromptStore) Progress(_ context.Context, _ *models.DirectorJob, fn func(*models.DirectorProject) error) error {
	return fn(&s.row)
}
func (s *seriesPromptStore) Complete(_ context.Context, _ *models.DirectorJob, _ string, _ bool, fn func(*models.DirectorProject) error) error {
	return fn(&s.row)
}
func (s *seriesPromptStore) SeriesContext(_ context.Context, series, user string, before int) (*repo.DirectorSeriesContext, error) {
	s.contextArgs = []any{series, user, before}
	return s.context, s.contextErr
}

type seriesPromptAnalyzer struct{ prompt string }

func (a *seriesPromptAnalyzer) Analyze(_ context.Context, _ string, prompt string) (string, error) {
	a.prompt = prompt
	return "", errors.New("stop after capturing the prompt")
}

// storyPromptFixture mirrors the fixture used to record testdata/director_story_prompt_standalone.golden
// from the pre-series code, so any drift in standalone planner input fails the comparison.
func storyPromptFixture(t *testing.T) (*DirectorFacade, *seriesPromptStore, *seriesPromptAnalyzer) {
	t.Helper()
	at := time.Date(2026, 11, 8, 1, 2, 3, 0, time.UTC)
	p := dto.DirectorProject{ID: "11111111-1111-4111-8111-111111111111", Revision: 3, Name: "ดาว", AspectRatio: "16:9", Style: "anime", Resolution: "1080p", Step: "plot", CreatedAt: at, UpdatedAt: at,
		Idea:       dto.DirectorIdea{Prompt: "เด็กน้อยออกเดินทางตามหาดวงดาว", Genre: "adventure", Moods: []string{"อบอุ่น"}, SceneCount: 1, CharacterAssetIDs: []string{}, StoryModelID: "director-story"},
		Story:      dto.DirectorStory{Status: "queued", TotalScenes: 1, PointsCharged: 4},
		Storyboard: dto.DirectorStoryboard{Status: "idle", ImageModelID: "director-image", StyleReference: "style"},
		Scenes:     []dto.DirectorScene{{ID: "22222222-2222-4222-8222-222222222222", Index: 1, Status: "pending", ShotCount: 3, CharacterAssetIDs: []string{}, Shots: []dto.DirectorShot{}}}}
	d := directorDocument{Project: p, Keys: map[string]string{}, Generations: map[string]string{"story": "gen-1"}}
	s := &seriesPromptStore{}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	s.row.Document = b
	s.row.UserID = uuid.MustParse("44444444-4444-4444-8444-444444444444")
	snap, err := json.Marshal(directorJobInput{Project: p, Generation: "gen-1", Model: "fixture-story", ImageModel: "fixture-image"})
	if err != nil {
		t.Fatal(err)
	}
	s.job = models.DirectorJob{Base: models.Base{ID: uuid.MustParse("33333333-3333-4333-8333-333333333333")}, UserID: s.row.UserID, ProjectID: uuid.MustParse(p.ID), Kind: "story", TargetID: "story", Status: "queued", Snapshot: snap, Cost: 4}
	a := &seriesPromptAnalyzer{}
	f := NewDirectorFacade(s, nil, nil, a, nil, nil, nil, nil, DirectorOptions{Enabled: true, AnalysisModel: "fixture-story", ImageModel: "fixture-image", StoryPoints: 4, ImagePoints: 3, VideoPoints: 20})
	return f, s, a
}

func TestDirectorStandaloneStoryPromptByteIdentical(t *testing.T) {
	t.Parallel()
	want, err := os.ReadFile(filepath.Join("testdata", "director_story_prompt_standalone.golden"))
	if err != nil {
		t.Fatal(err)
	}
	f, s, a := storyPromptFixture(t)
	_ = f.Tick(context.Background())
	if a.prompt != string(want) {
		t.Fatalf("standalone story prompt changed:\n got: %s\nwant: %s", a.prompt, want)
	}
	if s.contextArgs != nil {
		t.Fatal("standalone project must not look up series context")
	}
}

func TestDirectorEpisodeStoryPromptCarriesSeriesContext(t *testing.T) {
	t.Parallel()
	f, s, a := storyPromptFixture(t)
	series := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	s.row.SeriesID, s.row.EpisodeNumber, s.row.EpisodeTitle = &series, directorPointer(7), directorPointer("การกลับมา")
	earlier := []repo.DirectorEpisodeBrief{}
	for n := 1; n <= 6; n++ {
		earlier = append(earlier, repo.DirectorEpisodeBrief{Number: n, Title: "ตอนเก่า" + strings.Repeat("ก", n), Logline: strings.Repeat("ล", 400)})
	}
	s.context = &repo.DirectorSeriesContext{SeriesName: "ดวงดาว", Earlier: earlier}
	_ = f.Tick(context.Background())
	if len(s.contextArgs) != 3 || s.contextArgs[0] != series.String() || s.contextArgs[1] != s.row.UserID.String() || s.contextArgs[2] != 7 {
		t.Fatalf("series context lookup args: %v", s.contextArgs)
	}
	i := strings.Index(a.prompt, "Project and instruction data: ")
	if i < 0 {
		t.Fatal("planner data missing")
	}
	var data struct {
		Project map[string]any       `json:"project"`
		Series  *directorSeriesBrief `json:"series"`
	}
	if err := json.Unmarshal([]byte(a.prompt[i+len("Project and instruction data: "):]), &data); err != nil {
		t.Fatal(err)
	}
	if _, ok := data.Project["seriesId"]; ok {
		t.Fatal("episode identity leaked into project block")
	}
	sb := data.Series
	if sb == nil || sb.Name != "ดวงดาว" || sb.EpisodeNumber != 7 || sb.EpisodeTitle != "การกลับมา" || sb.Note == "" {
		t.Fatalf("series brief: %+v", sb)
	}
	if len(sb.EarlierEpisodes) != 5 || sb.EarlierEpisodes[0].Number != 2 || sb.EarlierEpisodes[4].Number != 6 {
		t.Fatalf("earlier episodes must be the 5 closest in ascending order: %+v", sb.EarlierEpisodes)
	}
	for _, ep := range sb.EarlierEpisodes {
		if utf8.RuneCountInString(ep.Logline) != directorSeriesContextLoglineMax {
			t.Fatalf("logline not capped: %d", utf8.RuneCountInString(ep.Logline))
		}
	}
}

func TestDirectorEpisodeStoryPromptDegradesWithoutContext(t *testing.T) {
	t.Parallel()
	f, s, a := storyPromptFixture(t)
	series := uuid.New()
	s.row.SeriesID, s.row.EpisodeNumber = &series, directorPointer(2)
	s.contextErr = errors.New("database unavailable")
	_ = f.Tick(context.Background())
	want, err := os.ReadFile(filepath.Join("testdata", "director_story_prompt_standalone.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if a.prompt != string(want) {
		t.Fatal("failed context lookup must fall back to the standalone prompt")
	}
}

type seriesFacadeStore struct {
	directorStore
	series       models.DirectorSeries
	assets       map[string]*models.LibraryAsset
	created      *models.DirectorProject
	createKey    string
	deleteErr    error
	changeCall   int
	found        bool
	episodes     []models.DirectorProject
	episodeLoads int
	stats        map[uuid.UUID]repo.DirectorSeriesStats
}

func (s *seriesFacadeStore) load(id string) (*models.LibraryAsset, error) {
	if a, ok := s.assets[id]; ok {
		return a, nil
	}
	return nil, repo.ErrNotFound
}
func (s *seriesFacadeStore) CreateSeries(_ context.Context, m *models.DirectorSeries, key, _ string, validate func(*models.DirectorSeries, repo.DirectorAssetLoader) error) (*models.DirectorSeries, error) {
	s.createKey = key
	if e := validate(m, s.load); e != nil {
		return nil, e
	}
	s.series = *m
	return m, nil
}
func (s *seriesFacadeStore) SeriesEpisodes(context.Context, string, []uuid.UUID) ([]models.DirectorProject, error) {
	s.episodeLoads++
	return append([]models.DirectorProject{}, s.episodes...), nil
}
func (s *seriesFacadeStore) SeriesStats(_ context.Context, _ string, ids []uuid.UUID) (map[uuid.UUID]repo.DirectorSeriesStats, error) {
	out := map[uuid.UUID]repo.DirectorSeriesStats{}
	for _, id := range ids {
		if st, ok := s.stats[id]; ok {
			out[id] = st
		}
	}
	return out, nil
}
func (s *seriesFacadeStore) ListSeries(context.Context, string, string, int, int) ([]models.DirectorSeries, int64, error) {
	return []models.DirectorSeries{s.series}, 1, nil
}
func (s *seriesFacadeStore) GetSeries(context.Context, string, string) (*models.DirectorSeries, error) {
	if !s.found {
		return nil, repo.ErrNotFound
	}
	series := s.series
	return &series, nil
}
func (s *seriesFacadeStore) ChangeSeries(_ context.Context, _, _ string, fn func(*models.DirectorSeries, repo.DirectorAssetLoader) error) (*models.DirectorSeries, error) {
	s.changeCall++
	next := s.series
	if e := fn(&next, s.load); e != nil {
		return nil, e
	}
	s.series = next
	return &next, nil
}
func (s *seriesFacadeStore) DeleteSeries(context.Context, string, string) error {
	return s.deleteErr
}
func (s *seriesFacadeStore) CreateEpisode(_ context.Context, _, _, key, _ string, build func(*models.DirectorSeries, int, repo.DirectorAssetLoader) (*models.DirectorProject, error)) (*models.DirectorProject, error) {
	s.createKey = key
	p, e := build(&s.series, 4, s.load)
	if e != nil {
		return nil, e
	}
	p.SeriesID = &s.series.ID
	p.EpisodeNumber = directorPointer(4)
	s.created = p
	return p, nil
}

func newSeriesFacade() (*DirectorFacade, *seriesFacadeStore) {
	key := "director/character.png"
	s := &seriesFacadeStore{assets: map[string]*models.LibraryAsset{}}
	for _, id := range []string{"aaaaaaaa-0000-4000-8000-000000000001", "aaaaaaaa-0000-4000-8000-000000000002", "aaaaaaaa-0000-4000-8000-000000000003", "aaaaaaaa-0000-4000-8000-000000000004", "aaaaaaaa-0000-4000-8000-000000000005"} {
		s.assets[id] = &models.LibraryAsset{AssetType: "character", StorageKey: &key}
	}
	s.assets["bbbbbbbb-0000-4000-8000-000000000001"] = &models.LibraryAsset{AssetType: "background", StorageKey: &key}
	f := NewDirectorFacade(s, nil, &continuityMedia{objects: map[string][]byte{}}, nil, nil, nil, nil, nil, DirectorOptions{Enabled: true, AnalysisModel: "fixture-story", ImageModel: "fixture-image", StoryPoints: 4, ImagePoints: 3, VideoPoints: 20})
	return f, s
}

func TestDirectorCreateSeriesValidation(t *testing.T) {
	t.Parallel()
	valid := func() req.DirectorSeriesRequest {
		return req.DirectorSeriesRequest{IdempotencyKey: uuid.NewString(), Name: directorPointer("ซีรีส์"), AspectRatio: directorPointer("9:16"), Style: directorPointer("anime"), Resolution: directorPointer("720p")}
	}
	cases := []struct {
		name  string
		edit  func(*req.DirectorSeriesRequest)
		field string
	}{
		{"valid", func(*req.DirectorSeriesRequest) {}, ""},
		{"name 120 runes", func(r *req.DirectorSeriesRequest) { r.Name = directorPointer(strings.Repeat("ก", 120)) }, ""},
		{"name 121 runes", func(r *req.DirectorSeriesRequest) { r.Name = directorPointer(strings.Repeat("ก", 121)) }, "name"},
		{"blank name", func(r *req.DirectorSeriesRequest) { r.Name = directorPointer("   ") }, "name"},
		{"missing style", func(r *req.DirectorSeriesRequest) { r.Style = nil }, "request"},
		{"bad ratio", func(r *req.DirectorSeriesRequest) { r.AspectRatio = directorPointer("21:9") }, "aspectRatio"},
		{"bad style", func(r *req.DirectorSeriesRequest) { r.Style = directorPointer("noir") }, "style"},
		{"unavailable resolution", func(r *req.DirectorSeriesRequest) { r.Resolution = directorPointer("4k") }, "resolution"},
		{"bad key", func(r *req.DirectorSeriesRequest) { r.IdempotencyKey = "nope" }, "idempotencyKey"},
		{"cast of four", func(r *req.DirectorSeriesRequest) {
			r.CharacterAssetIDs = &[]string{"aaaaaaaa-0000-4000-8000-000000000001", "aaaaaaaa-0000-4000-8000-000000000002", "aaaaaaaa-0000-4000-8000-000000000003", "aaaaaaaa-0000-4000-8000-000000000004"}
		}, ""},
		{"cast of five", func(r *req.DirectorSeriesRequest) {
			r.CharacterAssetIDs = &[]string{"aaaaaaaa-0000-4000-8000-000000000001", "aaaaaaaa-0000-4000-8000-000000000002", "aaaaaaaa-0000-4000-8000-000000000003", "aaaaaaaa-0000-4000-8000-000000000004", "aaaaaaaa-0000-4000-8000-000000000005"}
		}, "characterAssetIds"},
		{"duplicate cast", func(r *req.DirectorSeriesRequest) {
			r.CharacterAssetIDs = &[]string{"aaaaaaaa-0000-4000-8000-000000000001", "aaaaaaaa-0000-4000-8000-000000000001"}
		}, "characterAssetIds"},
		{"non-character cast", func(r *req.DirectorSeriesRequest) {
			r.CharacterAssetIDs = &[]string{"bbbbbbbb-0000-4000-8000-000000000001"}
		}, "characterAssetIds"},
		{"unowned cast", func(r *req.DirectorSeriesRequest) { r.CharacterAssetIDs = &[]string{uuid.NewString()} }, "characterAssetIds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, _ := newSeriesFacade()
			in := valid()
			tc.edit(&in)
			out, err := f.CreateSeries(context.Background(), uuid.NewString(), in)
			var v *StoryFlowValidationError
			if tc.field == "" {
				if err != nil || out.Revision != 1 || out.EpisodeCount != 0 || out.CharacterAssetIDs == nil || out.CoverImageURL != nil {
					t.Fatalf("want success, got %+v %v", out, err)
				}
				return
			}
			if !errors.As(err, &v) || v.Field != tc.field {
				t.Fatalf("want validation error on %s, got %v", tc.field, err)
			}
		})
	}
}

func TestDirectorUpdateSeriesRevisionAndDefaults(t *testing.T) {
	t.Parallel()
	f, s := newSeriesFacade()
	s.series = models.DirectorSeries{Base: models.Base{ID: uuid.New()}, Name: "เดิม", AspectRatio: "16:9", Style: "anime", Resolution: "1080p", CharacterAssetIDs: []byte("[]"), Revision: 2}
	id := s.series.ID.String()
	if _, err := f.UpdateSeries(context.Background(), id, uuid.NewString(), req.DirectorSeriesRequest{Name: directorPointer("ใหม่")}); !isValidation(err, "revision") {
		t.Fatalf("missing revision: %v", err)
	}
	_, err := f.UpdateSeries(context.Background(), id, uuid.NewString(), req.DirectorSeriesRequest{Revision: directorPointer(1), Name: directorPointer("ใหม่")})
	var d *DirectorError
	if !errors.As(err, &d) || d.Status != 409 || d.Code != "REVISION_CONFLICT" || d.CurrentRevision == nil || *d.CurrentRevision != 2 {
		t.Fatalf("stale revision: %v", err)
	}
	if _, err = f.UpdateSeries(context.Background(), id, uuid.NewString(), req.DirectorSeriesRequest{Revision: directorPointer(2), Style: directorPointer("noir")}); !isValidation(err, "style") || s.series.Style != "anime" {
		t.Fatalf("invalid style must not persist: %v", err)
	}
	out, err := f.UpdateSeries(context.Background(), id, uuid.NewString(), req.DirectorSeriesRequest{Revision: directorPointer(2), Name: directorPointer("ใหม่"), CharacterAssetIDs: &[]string{"aaaaaaaa-0000-4000-8000-000000000001"}})
	if err != nil || out.Revision != 3 || out.Name != "ใหม่" || len(out.CharacterAssetIDs) != 1 {
		t.Fatalf("update: %+v %v", out, err)
	}
	if _, err = f.UpdateSeries(context.Background(), "not-a-uuid", uuid.NewString(), req.DirectorSeriesRequest{Revision: directorPointer(3)}); !errors.Is(err, ErrDirectorProjectNotFound) {
		t.Fatalf("malformed id must be 404: %v", err)
	}
}

func isValidation(err error, field string) bool {
	var v *StoryFlowValidationError
	return errors.As(err, &v) && v.Field == field
}

func TestDirectorSeriesNotFoundAndBusy(t *testing.T) {
	t.Parallel()
	f, s := newSeriesFacade()
	c := context.Background()
	if _, err := f.GetSeries(c, "x", uuid.NewString()); !errors.Is(err, ErrDirectorProjectNotFound) {
		t.Fatalf("malformed get: %v", err)
	}
	if _, err := f.GetSeries(c, uuid.NewString(), uuid.NewString()); !errors.Is(err, ErrDirectorProjectNotFound) {
		t.Fatalf("unowned get: %v", err)
	}
	if err := f.DeleteSeries(c, "x", uuid.NewString()); !errors.Is(err, ErrDirectorProjectNotFound) {
		t.Fatalf("malformed delete: %v", err)
	}
	s.deleteErr = repo.ErrDirectorSeriesBusy
	var d *DirectorError
	if err := f.DeleteSeries(c, uuid.NewString(), uuid.NewString()); !errors.As(err, &d) || d.Status != 409 || d.Code != "SERIES_BUSY" {
		t.Fatalf("busy delete: %v", err)
	}
	if _, err := f.CreateEpisode(c, "x", uuid.NewString(), req.DirectorEpisodeRequest{IdempotencyKey: uuid.NewString()}); !errors.Is(err, ErrDirectorProjectNotFound) {
		t.Fatalf("malformed episode series: %v", err)
	}
	if _, err := f.ListSeries(c, uuid.NewString(), DirectorModeDirector, 1, 51); !isValidation(err, "pagination") {
		t.Fatalf("limit max: %v", err)
	}
}

func TestDirectorCreateEpisodeCopiesSeriesDefaults(t *testing.T) {
	t.Parallel()
	f, s := newSeriesFacade()
	cast := []string{"aaaaaaaa-0000-4000-8000-000000000001", uuid.NewString()}
	raw, _ := json.Marshal(cast)
	s.series = models.DirectorSeries{Base: models.Base{ID: uuid.New()}, Name: strings.Repeat("ย", 120), AspectRatio: "9:16", Style: "watercolor", Resolution: "720p", CharacterAssetIDs: raw, Revision: 1}
	user := uuid.NewString()
	c := context.Background()
	if _, err := f.CreateEpisode(c, s.series.ID.String(), user, req.DirectorEpisodeRequest{IdempotencyKey: "bad"}); !isValidation(err, "idempotencyKey") {
		t.Fatalf("key: %v", err)
	}
	if _, err := f.CreateEpisode(c, s.series.ID.String(), user, req.DirectorEpisodeRequest{IdempotencyKey: uuid.NewString(), Title: directorPointer(strings.Repeat("ต", 121))}); !isValidation(err, "title") {
		t.Fatalf("title length: %v", err)
	}
	key := uuid.NewString()
	p, err := f.CreateEpisode(c, s.series.ID.String(), user, req.DirectorEpisodeRequest{IdempotencyKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if s.createKey != key || p.Step != "idea" || p.AspectRatio != "9:16" || p.Style != "watercolor" || p.Resolution != "720p" {
		t.Fatalf("defaults not copied: %+v", p)
	}
	if len(p.Idea.CharacterAssetIDs) != 1 || p.Idea.CharacterAssetIDs[0] != cast[0] {
		t.Fatalf("cast must copy only still-valid characters: %v", p.Idea.CharacterAssetIDs)
	}
	if p.SeriesID == nil || *p.SeriesID != s.series.ID.String() || p.EpisodeNumber == nil || *p.EpisodeNumber != 4 || p.EpisodeTitle != nil {
		t.Fatalf("episode identity: %+v %+v %+v", p.SeriesID, p.EpisodeNumber, p.EpisodeTitle)
	}
	if utf8.RuneCountInString(p.Name) != 80 || !strings.HasSuffix(p.Name, " ตอนที่ 4") {
		t.Fatalf("generated name must fit the 80-rune project limit and keep the suffix: %q", p.Name)
	}
	var stored directorDocument
	if err = json.Unmarshal(s.created.Document, &stored); err != nil || stored.Project.SeriesID != nil {
		t.Fatal("episode identity must stay out of the stored document")
	}
	titled, err := f.CreateEpisode(c, s.series.ID.String(), user, req.DirectorEpisodeRequest{IdempotencyKey: uuid.NewString(), Title: directorPointer("  บทเริ่มต้น  ")})
	if err != nil || titled.Name != "บทเริ่มต้น" || titled.EpisodeTitle == nil || *titled.EpisodeTitle != "บทเริ่มต้น" {
		t.Fatalf("titled episode: %+v %v", titled, err)
	}
}

func TestDirectorEpisodeTitlePatchOnlyForEpisodes(t *testing.T) {
	t.Parallel()
	p, _, _, _ := directorNewTestRows(t)
	f := NewDirectorFacade(&patchStore{row: p}, nil, &continuityMedia{objects: map[string][]byte{}}, nil, nil, nil, nil, nil, DirectorOptions{})
	if _, err := f.Mutate(context.Background(), p.ID.String(), p.UserID.String(), "project", "", req.DirectorRequest{Revision: directorPointer(1), EpisodeTitle: directorPointer("x")}); !isValidation(err, "episodeTitle") {
		t.Fatalf("standalone episodeTitle: %v", err)
	}
	series := uuid.New()
	p.SeriesID, p.EpisodeNumber = &series, directorPointer(1)
	store := &patchStore{row: p}
	f = NewDirectorFacade(store, nil, &continuityMedia{objects: map[string][]byte{}}, nil, nil, nil, nil, nil, DirectorOptions{})
	if _, err := f.Mutate(context.Background(), p.ID.String(), p.UserID.String(), "project", "", req.DirectorRequest{Revision: directorPointer(1), EpisodeTitle: directorPointer(strings.Repeat("ก", 121))}); !isValidation(err, "episodeTitle") {
		t.Fatalf("title length: %v", err)
	}
	out, err := f.Mutate(context.Background(), p.ID.String(), p.UserID.String(), "project", "", req.DirectorRequest{Revision: directorPointer(1), EpisodeTitle: directorPointer("ตอนใหม่")})
	if err != nil || out.Revision != 2 || out.EpisodeTitle == nil || *out.EpisodeTitle != "ตอนใหม่" || out.SeriesID == nil || *out.EpisodeNumber != 1 {
		t.Fatalf("episode title patch: %+v %v", out, err)
	}
	out, err = f.Mutate(context.Background(), p.ID.String(), p.UserID.String(), "project", "", req.DirectorRequest{Revision: directorPointer(2), EpisodeTitle: directorPointer("")})
	if err != nil || out.EpisodeTitle != nil {
		t.Fatalf("empty title must clear: %+v %v", out.EpisodeTitle, err)
	}
}

type patchStore struct {
	directorStore
	row *models.DirectorProject
}

func (s *patchStore) Change(_ context.Context, _, _, _, _ string, fn func(*models.DirectorProject, func(string) (*models.LibraryAsset, error)) ([]models.DirectorJob, error)) (*models.DirectorProject, error) {
	next := *s.row
	if _, e := fn(&next, func(string) (*models.LibraryAsset, error) { return nil, repo.ErrNotFound }); e != nil {
		return nil, e
	}
	*s.row = next
	return &next, nil
}

func directorNewTestRows(t *testing.T) (*models.DirectorProject, dto.DirectorProject, *DirectorFacade, string) {
	t.Helper()
	f := NewDirectorFacade(nil, nil, nil, nil, nil, nil, nil, nil, DirectorOptions{})
	user := uuid.NewString()
	m, p, err := f.newProjectRow(user, "โปรเจกต์", "16:9", "anime", "1080p", []string{})
	if err != nil {
		t.Fatal(err)
	}
	return m, p, f, user
}

func seriesEpisodeRow(t *testing.T, series uuid.UUID, number int, d directorDocument) models.DirectorProject {
	t.Helper()
	m := models.DirectorProject{Base: models.Base{ID: uuid.New()}, SeriesID: &series, EpisodeNumber: directorPointer(number)}
	if err := directorEncode(&m, &d); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestDirectorSeriesListAndDetailSignOnlyCovers(t *testing.T) {
	t.Parallel()
	f, s := newSeriesFacade()
	var signed []string
	f.storage = &continuityMedia{objects: map[string][]byte{}, onSign: func(k string) { signed = append(signed, k) }}
	s.found = true
	s.series = models.DirectorSeries{Base: models.Base{ID: uuid.New()}, Name: "ซีรีส์", AspectRatio: "16:9", Style: "anime", Resolution: "1080p", CharacterAssetIDs: []byte("[]"), Revision: 1}
	s.stats = map[uuid.UUID]repo.DirectorSeriesStats{s.series.ID: {EpisodeCount: 2, CoverKey: "episode2/a.png"}}

	list, err := f.ListSeries(context.Background(), uuid.NewString(), DirectorModeDirector, 1, 20)
	if err != nil || len(list.Items) != 1 || list.Items[0].EpisodeCount != 2 || list.Items[0].CoverImageURL == nil || !strings.HasSuffix(*list.Items[0].CoverImageURL, "episode2/a.png") {
		t.Fatalf("list: %+v %v", list, err)
	}
	if s.episodeLoads != 0 || len(signed) != 1 {
		t.Fatalf("list must not load episode documents (%d loads) or sign more than the cover (%v)", s.episodeLoads, signed)
	}

	empty := directorTestProject()
	empty.Scenes = []dto.DirectorScene{}
	board := directorTestProject()
	board.Name, board.Step, board.Storyboard.Status, board.Storyboard.TotalShots = "ตอนสอง", "storyboard", "succeeded", 2
	for i := range board.Scenes[0].Shots {
		board.Scenes[0].Shots[i].Image.Status = "succeeded"
		board.Scenes[0].Shots[i].Video = &dto.DirectorVideo{Status: "succeeded"}
	}
	board.Export = &dto.DirectorExport{Status: "succeeded"}
	keys := map[string]string{"a": "episode2/a.png", "b": "episode2/b.png", "video:a": "episode2/a.mp4", "video:b": "episode2/b.mp4", "export": "episode2/export.mp4"}
	s.episodes = []models.DirectorProject{
		seriesEpisodeRow(t, s.series.ID, 1, directorDocument{Project: empty, Keys: map[string]string{}}),
		seriesEpisodeRow(t, s.series.ID, 2, directorDocument{Project: board, Keys: keys}),
	}
	signed = nil
	detail, err := f.GetSeries(context.Background(), s.series.ID.String(), uuid.NewString())
	if err != nil || detail.EpisodeCount != 2 || len(detail.Episodes) != 2 {
		t.Fatalf("detail: %+v %v", detail, err)
	}
	if len(signed) != 1 || signed[0] != "episode2/a.png" {
		t.Fatalf("detail must sign only the episode cover, signed %v", signed)
	}
	second := detail.Episodes[1]
	if detail.Episodes[0].CoverImageURL != nil || second.CoverImageURL == nil || detail.CoverImageURL == nil || *detail.CoverImageURL != *second.CoverImageURL {
		t.Fatalf("cover selection: %+v", detail)
	}
	if second.Name != "ตอนสอง" || second.Step != "storyboard" || second.TotalShots != 2 || second.EpisodeNumber == nil || *second.EpisodeNumber != 2 || second.SeriesID == nil || *second.SeriesID != s.series.ID.String() {
		t.Fatalf("summary fields: %+v", second)
	}

	board.Cover = &dto.DirectorCover{ID: "cover-1", Status: "succeeded", Source: "generated"}
	keys["cover-1"] = "episode2/cover.png"
	s.episodes[1] = seriesEpisodeRow(t, s.series.ID, 2, directorDocument{Project: board, Keys: keys})
	signed = nil
	if detail, err = f.GetSeries(context.Background(), s.series.ID.String(), uuid.NewString()); err != nil || len(signed) != 1 || signed[0] != "episode2/cover.png" {
		t.Fatalf("succeeded cover must win over shot images: %v %v", signed, err)
	}
}

func TestDirectorUpdateSeriesNoopKeepsRevision(t *testing.T) {
	t.Parallel()
	f, s := newSeriesFacade()
	s.series = models.DirectorSeries{Base: models.Base{ID: uuid.New()}, Name: "เดิม", AspectRatio: "16:9", Style: "anime", Resolution: "1080p", CharacterAssetIDs: []byte("[]"), Revision: 4}
	out, err := f.UpdateSeries(context.Background(), s.series.ID.String(), uuid.NewString(), req.DirectorSeriesRequest{Revision: directorPointer(4)})
	if err != nil || out.Revision != 4 {
		t.Fatalf("no-op patch must keep revision: %+v %v", out, err)
	}
}

// Series mode scopes listing to the menu that created the series (ผู้กำกับ or ภาพยนตร์) and never changes.
func TestDirectorSeriesModeScopesMenuAndIsFixed(t *testing.T) {
	t.Parallel()
	base := func() req.DirectorSeriesRequest {
		return req.DirectorSeriesRequest{IdempotencyKey: uuid.NewString(), Name: directorPointer("หนัง"), AspectRatio: directorPointer("16:9"), Style: directorPointer("anime"), Resolution: directorPointer("1080p")}
	}
	c := context.Background()
	f, s := newSeriesFacade()
	out, err := f.CreateSeries(c, uuid.NewString(), base())
	if err != nil || out.Mode != DirectorModeDirector || s.series.Mode != DirectorModeDirector {
		t.Fatalf("omitted mode must default to director: %+v %v", out, err)
	}
	in := base()
	in.Mode = directorPointer(DirectorModeFilm)
	if out, err = f.CreateSeries(c, uuid.NewString(), in); err != nil || out.Mode != DirectorModeFilm {
		t.Fatalf("film mode: %+v %v", out, err)
	}
	in = base()
	in.Mode = directorPointer("studio")
	if _, err = f.CreateSeries(c, uuid.NewString(), in); !isValidation(err, "mode") {
		t.Fatalf("unknown create mode: %v", err)
	}
	if _, err = f.ListSeries(c, uuid.NewString(), "studio", 1, 20); !isValidation(err, "mode") {
		t.Fatalf("unknown list mode: %v", err)
	}
	s.series.Revision = 1
	if _, err = f.UpdateSeries(c, uuid.NewString(), uuid.NewString(), req.DirectorSeriesRequest{Revision: directorPointer(1), Mode: directorPointer(DirectorModeDirector)}); !isValidation(err, "mode") {
		t.Fatalf("mode change must be rejected: %v", err)
	}
	out, err = f.UpdateSeries(c, uuid.NewString(), uuid.NewString(), req.DirectorSeriesRequest{Revision: directorPointer(1), Mode: directorPointer(DirectorModeFilm), Name: directorPointer("ใหม่")})
	if err != nil || out.Mode != DirectorModeFilm || out.Name != "ใหม่" {
		t.Fatalf("same mode resend: %+v %v", out, err)
	}
}
