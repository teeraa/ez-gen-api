package facade_open_api

import (
	"context"
	"fmt"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/google/uuid"
)

// DNAFacade handles character profiles, style profiles, scenes, and tags.
type DNAFacade interface {
	// Tags
	ListTags(ctx context.Context, category string) ([]dto_open_api.TagDTO, error)
	ListTagCategories(ctx context.Context) []string

	// Character profiles
	CreateCharacterProfile(ctx context.Context, userID string, in CreateCharacterProfileInput) (*dto_open_api.CharacterProfileDTO, error)
	GetCharacterProfile(ctx context.Context, id, userID string) (*dto_open_api.CharacterProfileDTO, error)
	ListCharacterProfiles(ctx context.Context, userID string, page, limit int) (*dto_open_api.CharacterProfileListDTO, error)
	UpdateCharacterProfile(ctx context.Context, id, userID string, in UpdateCharacterProfileInput) (*dto_open_api.CharacterProfileDTO, error)
	DeleteCharacterProfile(ctx context.Context, id, userID string) error

	// Style profiles
	CreateStyleProfile(ctx context.Context, userID string, in CreateStyleProfileInput) (*dto_open_api.StyleProfileDTO, error)
	GetStyleProfile(ctx context.Context, id, userID string) (*dto_open_api.StyleProfileDTO, error)
	ListStyleProfiles(ctx context.Context, userID string, page, limit int) (*dto_open_api.StyleProfileListDTO, error)
	UpdateStyleProfile(ctx context.Context, id, userID string, in UpdateStyleProfileInput) (*dto_open_api.StyleProfileDTO, error)
	DeleteStyleProfile(ctx context.Context, id, userID string) error

	// Scenes
	CreateScene(ctx context.Context, userID string, in CreateSceneInput) (*dto_open_api.SceneDTO, error)
	GetScene(ctx context.Context, id, userID string) (*dto_open_api.SceneDTO, error)
	ListScenes(ctx context.Context, userID string, page, limit int) (*dto_open_api.SceneListDTO, error)
	UpdateScene(ctx context.Context, id, userID string, in UpdateSceneInput) (*dto_open_api.SceneDTO, error)
	DeleteScene(ctx context.Context, id, userID string) error
	SelectSceneOutput(ctx context.Context, sceneID, jobID, userID string) error
}

// ─── Input types ─────────────────────────────────────────────────────────────

type CreateCharacterProfileInput struct {
	Name             string
	Description      string
	Appearance       string
	Hairstyle        string
	Clothing         string
	Accessories      string
	DefiningFeatures string
	ConsistencyNotes string
	TagIDs           []string
}

type UpdateCharacterProfileInput struct {
	Name             *string
	Description      *string
	Appearance       *string
	Hairstyle        *string
	Clothing         *string
	Accessories      *string
	DefiningFeatures *string
	ConsistencyNotes *string
	TagIDs           []string
}

type CreateStyleProfileInput struct {
	Name           string
	Description    string
	VisualStyle    string
	ColorPalette   string
	Lighting       string
	Mood           string
	CameraLanguage string
	QualityNotes   string
	TagIDs         []string
}

type UpdateStyleProfileInput struct {
	Name           *string
	Description    *string
	VisualStyle    *string
	ColorPalette   *string
	Lighting       *string
	Mood           *string
	CameraLanguage *string
	QualityNotes   *string
	TagIDs         []string
}

type CreateSceneInput struct {
	Title               string
	Description         string
	CharacterProfileID  string
	CharacterProfileIDs []string
	StyleProfileID      string
	PreviousSceneID     string
	SceneOrder          int
	ActionOverride      string
	CameraOverride      string
	SettingOverride     string
	TagIDs              []string
}

type UpdateSceneInput struct {
	Title       *string
	Description *string
	// CharacterProfileIDs replaces the whole list when non-nil; an empty slice clears it.
	// The legacy CharacterProfileID also replaces the whole list, with a single entry.
	CharacterProfileIDs []string
	CharacterProfileID  *string
	StyleProfileID      *string
	SceneOrder          *int
	ActionOverride      *string
	CameraOverride      *string
	SettingOverride     *string
	TagIDs              []string
}

// ─── Implementation ───────────────────────────────────────────────────────────

type dnaFacade struct {
	tags       repository_open_api.TagRepository
	characters repository_open_api.CharacterProfileRepository
	styles     repository_open_api.StyleProfileRepository
	scenes     repository_open_api.SceneRepository
	jobs       repository_open_api.JobRepository
	storage    mediaStorage
}

func NewDNAFacade(
	tags repository_open_api.TagRepository,
	characters repository_open_api.CharacterProfileRepository,
	styles repository_open_api.StyleProfileRepository,
	scenes repository_open_api.SceneRepository,
	jobs repository_open_api.JobRepository,
	storage mediaStorage,
) DNAFacade {
	return &dnaFacade{
		tags:       tags,
		characters: characters,
		styles:     styles,
		scenes:     scenes,
		jobs:       jobs,
		storage:    storage,
	}
}

// ── Tags ──────────────────────────────────────────────────────────────────────

func (f *dnaFacade) ListTags(ctx context.Context, category string) ([]dto_open_api.TagDTO, error) {
	var (
		tags []models.DNATag
		err  error
	)
	if category != "" {
		tags, err = f.tags.ListByCategory(ctx, models.TagCategory(category))
	} else {
		tags, err = f.tags.ListAll(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	dtos := make([]dto_open_api.TagDTO, len(tags))
	for i, t := range tags {
		dtos[i] = mapTagToDTO(t)
	}
	return dtos, nil
}

func (f *dnaFacade) ListTagCategories(_ context.Context) []string {
	cats := make([]string, len(models.AllTagCategories))
	for i, c := range models.AllTagCategories {
		cats[i] = string(c)
	}
	return cats
}

// ── Character Profiles ────────────────────────────────────────────────────────

func (f *dnaFacade) CreateCharacterProfile(ctx context.Context, userID string, in CreateCharacterProfileInput) (*dto_open_api.CharacterProfileDTO, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrUnauthorized
	}

	profile := &models.CharacterProfile{
		UserID:           uid,
		Name:             in.Name,
		Description:      strPtr(in.Description),
		Appearance:       strPtr(in.Appearance),
		Hairstyle:        strPtr(in.Hairstyle),
		Clothing:         strPtr(in.Clothing),
		Accessories:      strPtr(in.Accessories),
		DefiningFeatures: strPtr(in.DefiningFeatures),
		ConsistencyNotes: strPtr(in.ConsistencyNotes),
	}

	if err := f.characters.Create(ctx, profile); err != nil {
		return nil, fmt.Errorf("create character profile: %w", err)
	}

	if len(in.TagIDs) > 0 {
		tagUUIDs, err := parseUUIDs(in.TagIDs)
		if err != nil {
			return nil, ErrValidation
		}
		if err := f.characters.SetTags(ctx, profile.ID, tagUUIDs); err != nil {
			return nil, fmt.Errorf("set character profile tags: %w", err)
		}
	}

	return f.GetCharacterProfile(ctx, profile.ID.String(), userID)
}

func (f *dnaFacade) GetCharacterProfile(ctx context.Context, id, userID string) (*dto_open_api.CharacterProfileDTO, error) {
	p, err := f.characters.FindByID(ctx, id, userID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	dto := mapCharacterProfileToDTO(*p)
	return &dto, nil
}

func (f *dnaFacade) ListCharacterProfiles(ctx context.Context, userID string, page, limit int) (*dto_open_api.CharacterProfileListDTO, error) {
	profiles, total, err := f.characters.List(ctx, userID, page, limit)
	if err != nil {
		return nil, fmt.Errorf("list character profiles: %w", err)
	}
	items := make([]dto_open_api.CharacterProfileDTO, len(profiles))
	for i, p := range profiles {
		items[i] = mapCharacterProfileToDTO(p)
	}
	return &dto_open_api.CharacterProfileListDTO{
		Items: items,
		Total: int(total),
		Page:  page,
		Limit: limit,
	}, nil
}

func (f *dnaFacade) UpdateCharacterProfile(ctx context.Context, id, userID string, in UpdateCharacterProfileInput) (*dto_open_api.CharacterProfileDTO, error) {
	p, err := f.characters.FindByID(ctx, id, userID)
	if err != nil {
		return nil, mapRepoErr(err)
	}

	applyStringPtr(&p.Name, in.Name)
	applyOptStringPtr(&p.Description, in.Description)
	applyOptStringPtr(&p.Appearance, in.Appearance)
	applyOptStringPtr(&p.Hairstyle, in.Hairstyle)
	applyOptStringPtr(&p.Clothing, in.Clothing)
	applyOptStringPtr(&p.Accessories, in.Accessories)
	applyOptStringPtr(&p.DefiningFeatures, in.DefiningFeatures)
	applyOptStringPtr(&p.ConsistencyNotes, in.ConsistencyNotes)
	p.Version++

	if err := f.characters.Update(ctx, p); err != nil {
		return nil, fmt.Errorf("update character profile: %w", err)
	}

	if in.TagIDs != nil {
		tagUUIDs, err := parseUUIDs(in.TagIDs)
		if err != nil {
			return nil, ErrValidation
		}
		if err := f.characters.SetTags(ctx, p.ID, tagUUIDs); err != nil {
			return nil, fmt.Errorf("set character profile tags: %w", err)
		}
	}

	return f.GetCharacterProfile(ctx, id, userID)
}

func (f *dnaFacade) DeleteCharacterProfile(ctx context.Context, id, userID string) error {
	if err := f.characters.Delete(ctx, id, userID); err != nil {
		return mapRepoErr(err)
	}
	return nil
}

// ── Style Profiles ────────────────────────────────────────────────────────────

func (f *dnaFacade) CreateStyleProfile(ctx context.Context, userID string, in CreateStyleProfileInput) (*dto_open_api.StyleProfileDTO, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrUnauthorized
	}

	profile := &models.StyleProfile{
		UserID:         uid,
		Name:           in.Name,
		Description:    strPtr(in.Description),
		VisualStyle:    strPtr(in.VisualStyle),
		ColorPalette:   strPtr(in.ColorPalette),
		Lighting:       strPtr(in.Lighting),
		Mood:           strPtr(in.Mood),
		CameraLanguage: strPtr(in.CameraLanguage),
		QualityNotes:   strPtr(in.QualityNotes),
	}

	if err := f.styles.Create(ctx, profile); err != nil {
		return nil, fmt.Errorf("create style profile: %w", err)
	}

	if len(in.TagIDs) > 0 {
		tagUUIDs, err := parseUUIDs(in.TagIDs)
		if err != nil {
			return nil, ErrValidation
		}
		if err := f.styles.SetTags(ctx, profile.ID, tagUUIDs); err != nil {
			return nil, fmt.Errorf("set style profile tags: %w", err)
		}
	}

	return f.GetStyleProfile(ctx, profile.ID.String(), userID)
}

func (f *dnaFacade) GetStyleProfile(ctx context.Context, id, userID string) (*dto_open_api.StyleProfileDTO, error) {
	p, err := f.styles.FindByID(ctx, id, userID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	dto := mapStyleProfileToDTO(*p)
	return &dto, nil
}

func (f *dnaFacade) ListStyleProfiles(ctx context.Context, userID string, page, limit int) (*dto_open_api.StyleProfileListDTO, error) {
	profiles, total, err := f.styles.List(ctx, userID, page, limit)
	if err != nil {
		return nil, fmt.Errorf("list style profiles: %w", err)
	}
	items := make([]dto_open_api.StyleProfileDTO, len(profiles))
	for i, p := range profiles {
		items[i] = mapStyleProfileToDTO(p)
	}
	return &dto_open_api.StyleProfileListDTO{
		Items: items,
		Total: int(total),
		Page:  page,
		Limit: limit,
	}, nil
}

func (f *dnaFacade) UpdateStyleProfile(ctx context.Context, id, userID string, in UpdateStyleProfileInput) (*dto_open_api.StyleProfileDTO, error) {
	p, err := f.styles.FindByID(ctx, id, userID)
	if err != nil {
		return nil, mapRepoErr(err)
	}

	applyStringPtr(&p.Name, in.Name)
	applyOptStringPtr(&p.Description, in.Description)
	applyOptStringPtr(&p.VisualStyle, in.VisualStyle)
	applyOptStringPtr(&p.ColorPalette, in.ColorPalette)
	applyOptStringPtr(&p.Lighting, in.Lighting)
	applyOptStringPtr(&p.Mood, in.Mood)
	applyOptStringPtr(&p.CameraLanguage, in.CameraLanguage)
	applyOptStringPtr(&p.QualityNotes, in.QualityNotes)

	if err := f.styles.Update(ctx, p); err != nil {
		return nil, fmt.Errorf("update style profile: %w", err)
	}

	if in.TagIDs != nil {
		tagUUIDs, err := parseUUIDs(in.TagIDs)
		if err != nil {
			return nil, ErrValidation
		}
		if err := f.styles.SetTags(ctx, p.ID, tagUUIDs); err != nil {
			return nil, fmt.Errorf("set style profile tags: %w", err)
		}
	}

	return f.GetStyleProfile(ctx, id, userID)
}

func (f *dnaFacade) DeleteStyleProfile(ctx context.Context, id, userID string) error {
	if err := f.styles.Delete(ctx, id, userID); err != nil {
		return mapRepoErr(err)
	}
	return nil
}

// ── Scenes ────────────────────────────────────────────────────────────────────

func (f *dnaFacade) CreateScene(ctx context.Context, userID string, in CreateSceneInput) (*dto_open_api.SceneDTO, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrUnauthorized
	}

	scene := &models.Scene{
		UserID:          uid,
		Title:           strPtr(in.Title),
		Description:     strPtr(in.Description),
		SceneOrder:      in.SceneOrder,
		ActionOverride:  strPtr(in.ActionOverride),
		CameraOverride:  strPtr(in.CameraOverride),
		SettingOverride: strPtr(in.SettingOverride),
	}

	characterIDs, err := parseCharacterIDs(requestedCharacterIDs(in.CharacterProfileIDs, &in.CharacterProfileID))
	if err != nil {
		return nil, err
	}
	if _, err := loadOwnedCharacters(ctx, f.characters, characterIDs, userID); err != nil {
		return nil, err
	}
	if len(characterIDs) > 0 {
		scene.CharacterProfileID = &characterIDs[0]
	}
	if in.StyleProfileID != "" {
		spID, err := uuid.Parse(in.StyleProfileID)
		if err != nil {
			return nil, ErrValidation
		}
		scene.StyleProfileID = &spID
	}
	if in.PreviousSceneID != "" {
		prevID, err := uuid.Parse(in.PreviousSceneID)
		if err != nil {
			return nil, ErrValidation
		}
		scene.PreviousSceneID = &prevID
	}

	if err := f.scenes.Create(ctx, scene); err != nil {
		return nil, fmt.Errorf("create scene: %w", err)
	}
	if len(characterIDs) > 0 {
		if err := f.scenes.SetCharacters(ctx, scene.ID, characterIDs); err != nil {
			return nil, fmt.Errorf("set scene characters: %w", err)
		}
	}

	if len(in.TagIDs) > 0 {
		tagUUIDs, err := parseUUIDs(in.TagIDs)
		if err != nil {
			return nil, ErrValidation
		}
		if err := f.scenes.SetTags(ctx, scene.ID, tagUUIDs); err != nil {
			return nil, fmt.Errorf("set scene tags: %w", err)
		}
	}

	return f.GetScene(ctx, scene.ID.String(), userID)
}

func (f *dnaFacade) GetScene(ctx context.Context, id, userID string) (*dto_open_api.SceneDTO, error) {
	s, err := f.scenes.FindByID(ctx, id, userID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	dtos := []dto_open_api.SceneDTO{mapSceneToDTO(*s)}
	f.attachScenePreviews(ctx, userID, []models.Scene{*s}, dtos)
	return &dtos[0], nil
}

func (f *dnaFacade) ListScenes(ctx context.Context, userID string, page, limit int) (*dto_open_api.SceneListDTO, error) {
	scenes, total, err := f.scenes.List(ctx, userID, page, limit)
	if err != nil {
		return nil, fmt.Errorf("list scenes: %w", err)
	}
	items := make([]dto_open_api.SceneDTO, len(scenes))
	for i, s := range scenes {
		items[i] = mapSceneToDTO(s)
	}
	f.attachScenePreviews(ctx, userID, scenes, items)
	return &dto_open_api.SceneListDTO{
		Items: items,
		Total: int(total),
		Page:  page,
		Limit: limit,
	}, nil
}

func (f *dnaFacade) UpdateScene(ctx context.Context, id, userID string, in UpdateSceneInput) (*dto_open_api.SceneDTO, error) {
	s, err := f.scenes.FindByID(ctx, id, userID)
	if err != nil {
		return nil, mapRepoErr(err)
	}

	applyOptStringPtr(&s.Title, in.Title)
	applyOptStringPtr(&s.Description, in.Description)
	applyOptStringPtr(&s.ActionOverride, in.ActionOverride)
	applyOptStringPtr(&s.CameraOverride, in.CameraOverride)
	applyOptStringPtr(&s.SettingOverride, in.SettingOverride)

	if in.SceneOrder != nil {
		s.SceneOrder = *in.SceneOrder
	}
	var characterIDs []uuid.UUID
	replaceCharacters := in.CharacterProfileIDs != nil || in.CharacterProfileID != nil
	if replaceCharacters {
		characterIDs, err = parseCharacterIDs(requestedCharacterIDs(in.CharacterProfileIDs, in.CharacterProfileID))
		if err != nil {
			return nil, err
		}
		if _, err := loadOwnedCharacters(ctx, f.characters, characterIDs, userID); err != nil {
			return nil, err
		}
	}
	if in.StyleProfileID != nil {
		if *in.StyleProfileID == "" {
			s.StyleProfileID = nil
		} else {
			spID, err := uuid.Parse(*in.StyleProfileID)
			if err != nil {
				return nil, ErrValidation
			}
			s.StyleProfileID = &spID
		}
	}

	if err := f.scenes.Update(ctx, s); err != nil {
		return nil, fmt.Errorf("update scene: %w", err)
	}
	// After Update, which writes back the old character_profile_id, so the sync wins.
	if replaceCharacters {
		if err := f.scenes.SetCharacters(ctx, s.ID, characterIDs); err != nil {
			return nil, fmt.Errorf("set scene characters: %w", err)
		}
	}

	if in.TagIDs != nil {
		tagUUIDs, err := parseUUIDs(in.TagIDs)
		if err != nil {
			return nil, ErrValidation
		}
		if err := f.scenes.SetTags(ctx, s.ID, tagUUIDs); err != nil {
			return nil, fmt.Errorf("set scene tags: %w", err)
		}
	}

	return f.GetScene(ctx, id, userID)
}

func (f *dnaFacade) DeleteScene(ctx context.Context, id, userID string) error {
	if err := f.scenes.Delete(ctx, id, userID); err != nil {
		return mapRepoErr(err)
	}
	return nil
}

func (f *dnaFacade) SelectSceneOutput(ctx context.Context, sceneID, jobID, userID string) error {
	sceneUUID, err := uuid.Parse(sceneID)
	if err != nil {
		return ErrValidation
	}
	jobUUID, err := uuid.Parse(jobID)
	if err != nil {
		return ErrValidation
	}
	// Without this, a scene could point at another user's job ID.
	if _, err := f.jobs.FindByID(ctx, jobID, userID); err != nil {
		return mapRepoErr(err)
	}
	if err := f.scenes.SetSelectedJob(ctx, sceneUUID, jobUUID, userID); err != nil {
		return mapRepoErr(err)
	}
	return nil
}

// ─── DTO mappers ─────────────────────────────────────────────────────────────

func mapTagToDTO(t models.DNATag) dto_open_api.TagDTO {
	return dto_open_api.TagDTO{
		ID:          t.ID.String(),
		Slug:        t.Slug,
		Name:        t.Name,
		Category:    string(t.Category),
		Description: strVal(t.Description),
		SortOrder:   t.SortOrder,
		CreatedAt:   t.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func mapCharacterProfileToDTO(p models.CharacterProfile) dto_open_api.CharacterProfileDTO {
	tags := make([]dto_open_api.TagDTO, len(p.Tags))
	for i, t := range p.Tags {
		tags[i] = mapTagToDTO(t)
	}
	return dto_open_api.CharacterProfileDTO{
		ID:               p.ID.String(),
		Name:             p.Name,
		Description:      strVal(p.Description),
		Appearance:       strVal(p.Appearance),
		Hairstyle:        strVal(p.Hairstyle),
		Clothing:         strVal(p.Clothing),
		Accessories:      strVal(p.Accessories),
		DefiningFeatures: strVal(p.DefiningFeatures),
		ConsistencyNotes: strVal(p.ConsistencyNotes),
		Version:          p.Version,
		Tags:             tags,
		CreatedAt:        p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func mapStyleProfileToDTO(p models.StyleProfile) dto_open_api.StyleProfileDTO {
	tags := make([]dto_open_api.TagDTO, len(p.Tags))
	for i, t := range p.Tags {
		tags[i] = mapTagToDTO(t)
	}
	return dto_open_api.StyleProfileDTO{
		ID:             p.ID.String(),
		Name:           p.Name,
		Description:    strVal(p.Description),
		VisualStyle:    strVal(p.VisualStyle),
		ColorPalette:   strVal(p.ColorPalette),
		Lighting:       strVal(p.Lighting),
		Mood:           strVal(p.Mood),
		CameraLanguage: strVal(p.CameraLanguage),
		QualityNotes:   strVal(p.QualityNotes),
		Tags:           tags,
		CreatedAt:      p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      p.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func mapSceneToDTO(s models.Scene) dto_open_api.SceneDTO {
	tags := make([]dto_open_api.TagDTO, len(s.Tags))
	for i, t := range s.Tags {
		tags[i] = mapTagToDTO(t)
	}
	characters := make([]dto_open_api.SceneCharacterDTO, 0, len(s.Characters))
	for _, c := range s.Characters {
		if c.CharacterProfile == nil {
			continue
		}
		characters = append(characters, dto_open_api.SceneCharacterDTO{
			ID:        c.CharacterProfileID.String(),
			Name:      c.CharacterProfile.Name,
			SortOrder: c.SortOrder,
		})
	}
	dto := dto_open_api.SceneDTO{
		Characters:  characters,
		ID:          s.ID.String(),
		Title:       strVal(s.Title),
		Description: strVal(s.Description),
		SceneOrder:  s.SceneOrder,
		Tags:        tags,
		CreatedAt:   s.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   s.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if s.CharacterProfileID != nil {
		v := s.CharacterProfileID.String()
		dto.CharacterProfileID = &v
	}
	if s.StyleProfileID != nil {
		v := s.StyleProfileID.String()
		dto.StyleProfileID = &v
	}
	if s.PreviousSceneID != nil {
		v := s.PreviousSceneID.String()
		dto.PreviousSceneID = &v
	}
	if s.SelectedJobID != nil {
		v := s.SelectedJobID.String()
		dto.SelectedJobID = &v
	}
	return dto
}

// ─── utility helpers ─────────────────────────────────────────────────────────

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func strVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func applyStringPtr(dst *string, src *string) {
	if src != nil {
		*dst = *src
	}
}

func applyOptStringPtr(dst **string, src *string) {
	if src != nil {
		if *src == "" {
			*dst = nil
		} else {
			*dst = src
		}
	}
}

func parseUUIDs(ids []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		u, err := uuid.Parse(id)
		if err != nil {
			return nil, fmt.Errorf("invalid uuid %q: %w", id, err)
		}
		out = append(out, u)
	}
	return out, nil
}

func mapRepoErr(err error) error {
	if err == nil {
		return nil
	}
	if err == repository_open_api.ErrNotFound {
		return ErrNotFound
	}
	return err
}
