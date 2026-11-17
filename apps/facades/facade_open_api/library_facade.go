package facade_open_api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/google/uuid"
)

var (
	ErrLibraryLimit    = errors.New("library item limit reached")
	ErrBuiltInReadOnly = errors.New("built-in library items cannot be changed")
)

// Per-user caps keep a single account from growing the shared tables without bound.
const (
	maxOwnPrompts    = 500
	maxOwnSkills     = 200
	defaultSkillIcon = "wand"
)

type LibraryQuery struct {
	Scope    string
	Category string
	Query    string
	Page     int
	Limit    int
}

type PromptInput struct {
	Title    string
	Category string
	Body     string
}

type SkillInput struct {
	Name         string
	Description  string
	Steps        []string
	Inputs       []string
	Output       string
	ForkedFromID *string
}

// LibraryFacade serves the Prompt library and Skills menus: built-ins plus each user's own items.
type LibraryFacade interface {
	ListPrompts(ctx context.Context, userID string, q LibraryQuery) (*dto_open_api.PromptListDTO, error)
	GetPrompt(ctx context.Context, id, userID string) (*dto_open_api.PromptDTO, error)
	CreatePrompt(ctx context.Context, userID string, in PromptInput) (*dto_open_api.PromptDTO, error)
	UpdatePrompt(ctx context.Context, id, userID string, in PromptInput) (*dto_open_api.PromptDTO, error)
	DeletePrompt(ctx context.Context, id, userID string) error
	SetPromptBookmark(ctx context.Context, id, userID string, on bool) (*dto_open_api.PromptDTO, error)

	ListSkills(ctx context.Context, userID string, q LibraryQuery) (*dto_open_api.SkillListDTO, error)
	GetSkill(ctx context.Context, id, userID string) (*dto_open_api.SkillDTO, error)
	CreateSkill(ctx context.Context, userID string, in SkillInput) (*dto_open_api.SkillDTO, error)
	UpdateSkill(ctx context.Context, id, userID string, in SkillInput) (*dto_open_api.SkillDTO, error)
	DeleteSkill(ctx context.Context, id, userID string) error
}

type libraryFacade struct {
	repo repository_open_api.LibraryRepository
}

func NewLibraryFacade(repo repository_open_api.LibraryRepository) LibraryFacade {
	return &libraryFacade{repo: repo}
}

func libraryIDs(id, userID string) (uuid.UUID, uuid.UUID, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return uuid.Nil, uuid.Nil, ErrUnauthorized
	}
	// A malformed id cannot name any row; answering 404 avoids a Postgres cast error becoming a 500.
	rid, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, uuid.Nil, ErrNotFound
	}
	return rid, uid, nil
}

func libraryRepoErr(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, repository_open_api.ErrNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", op, err)
}

func libraryFilter(q LibraryQuery) repository_open_api.LibraryFilter {
	return repository_open_api.LibraryFilter{
		Scope: q.Scope, Category: q.Category, Query: strings.TrimSpace(q.Query),
		Limit: q.Limit, Offset: (q.Page - 1) * q.Limit,
	}
}

func libraryTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// ── Prompts ───────────────────────────────────────────────────────────────────

func normalizePrompt(in PromptInput) (PromptInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Body = strings.TrimSpace(in.Body)
	if utf8.RuneCountInString(in.Title) < 2 || utf8.RuneCountInString(in.Body) < 5 {
		return in, ErrValidation
	}
	return in, nil
}

func promptDTO(r repository_open_api.PromptRow) dto_open_api.PromptDTO {
	return dto_open_api.PromptDTO{
		ID: r.ID.String(), Title: r.Title, Category: r.Category, Body: r.Body,
		BuiltIn: r.UserID == nil, Bookmarked: r.Bookmarked,
		CreatedAt: libraryTime(r.CreatedAt), UpdatedAt: libraryTime(r.UpdatedAt),
	}
}

func (f *libraryFacade) ListPrompts(ctx context.Context, userID string, q LibraryQuery) (*dto_open_api.PromptListDTO, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	rows, total, err := f.repo.ListPrompts(ctx, uid, libraryFilter(q))
	if err != nil {
		return nil, fmt.Errorf("list prompts: %w", err)
	}
	items := make([]dto_open_api.PromptDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, promptDTO(r))
	}
	return &dto_open_api.PromptListDTO{Items: items, Total: total, Page: q.Page, Limit: q.Limit}, nil
}

func (f *libraryFacade) GetPrompt(ctx context.Context, id, userID string) (*dto_open_api.PromptDTO, error) {
	rid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return nil, err
	}
	row, err := f.repo.FindPrompt(ctx, rid, uid)
	if err != nil {
		return nil, libraryRepoErr("get prompt", err)
	}
	dto := promptDTO(*row)
	return &dto, nil
}

func (f *libraryFacade) CreatePrompt(ctx context.Context, userID string, in PromptInput) (*dto_open_api.PromptDTO, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if in, err = normalizePrompt(in); err != nil {
		return nil, err
	}
	n, err := f.repo.CountOwnPrompts(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("create prompt: %w", err)
	}
	if n >= maxOwnPrompts {
		return nil, ErrLibraryLimit
	}
	p := &models.PromptTemplate{UserID: &uid, Title: in.Title, Category: in.Category, Body: in.Body}
	if err := f.repo.CreatePrompt(ctx, p); err != nil {
		return nil, fmt.Errorf("create prompt: %w", err)
	}
	dto := promptDTO(repository_open_api.PromptRow{PromptTemplate: *p})
	return &dto, nil
}

// ownedPrompt distinguishes a built-in (403) from a missing or foreign prompt (404).
func (f *libraryFacade) ownedPrompt(ctx context.Context, rid, uid uuid.UUID) error {
	row, err := f.repo.FindPrompt(ctx, rid, uid)
	if err != nil {
		return libraryRepoErr("find prompt", err)
	}
	if row.UserID == nil {
		return ErrBuiltInReadOnly
	}
	return nil
}

func (f *libraryFacade) UpdatePrompt(ctx context.Context, id, userID string, in PromptInput) (*dto_open_api.PromptDTO, error) {
	rid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return nil, err
	}
	if in, err = normalizePrompt(in); err != nil {
		return nil, err
	}
	if err := f.ownedPrompt(ctx, rid, uid); err != nil {
		return nil, err
	}
	if err := f.repo.UpdatePrompt(ctx, rid, uid, repository_open_api.PromptFields{Title: in.Title, Category: in.Category, Body: in.Body}); err != nil {
		return nil, libraryRepoErr("update prompt", err)
	}
	return f.GetPrompt(ctx, id, userID)
}

func (f *libraryFacade) DeletePrompt(ctx context.Context, id, userID string) error {
	rid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return err
	}
	if err := f.ownedPrompt(ctx, rid, uid); err != nil {
		return err
	}
	return libraryRepoErr("delete prompt", f.repo.DeletePrompt(ctx, rid, uid))
}

func (f *libraryFacade) SetPromptBookmark(ctx context.Context, id, userID string, on bool) (*dto_open_api.PromptDTO, error) {
	rid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return nil, err
	}
	if _, err := f.repo.FindPrompt(ctx, rid, uid); err != nil {
		return nil, libraryRepoErr("bookmark prompt", err)
	}
	if err := f.repo.SetBookmark(ctx, uid, rid, on); err != nil {
		return nil, fmt.Errorf("bookmark prompt: %w", err)
	}
	return f.GetPrompt(ctx, id, userID)
}

// ── Skills ────────────────────────────────────────────────────────────────────

func normalizeSkill(in SkillInput) (SkillInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	steps := make([]string, 0, len(in.Steps))
	for _, s := range in.Steps {
		if s = strings.TrimSpace(s); s != "" {
			steps = append(steps, s)
		}
	}
	seen := make(map[string]bool, len(in.Inputs))
	inputs := make([]string, 0, len(in.Inputs))
	for _, x := range in.Inputs {
		if !seen[x] {
			seen[x] = true
			inputs = append(inputs, x)
		}
	}
	in.Steps, in.Inputs = steps, inputs
	if utf8.RuneCountInString(in.Name) < 2 || len(steps) == 0 {
		return in, ErrValidation
	}
	return in, nil
}

func skillDTO(s models.Skill) (dto_open_api.SkillDTO, error) {
	dto := dto_open_api.SkillDTO{
		ID: s.ID.String(), Name: s.Name, Description: s.Description, Icon: s.Icon, Output: s.Output,
		BuiltIn: s.UserID == nil, Version: s.Version,
		CreatedAt: libraryTime(s.CreatedAt), UpdatedAt: libraryTime(s.UpdatedAt),
		Steps: []string{}, Inputs: []string{},
	}
	if s.ForkedFromID != nil {
		v := s.ForkedFromID.String()
		dto.ForkedFromID = &v
	}
	if err := json.Unmarshal(s.Steps, &dto.Steps); err != nil {
		return dto, fmt.Errorf("decode skill %s steps: %w", s.ID, err)
	}
	if len(s.Inputs) > 0 {
		if err := json.Unmarshal(s.Inputs, &dto.Inputs); err != nil {
			return dto, fmt.Errorf("decode skill %s inputs: %w", s.ID, err)
		}
	}
	return dto, nil
}

func (f *libraryFacade) ListSkills(ctx context.Context, userID string, q LibraryQuery) (*dto_open_api.SkillListDTO, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if q.Scope == repository_open_api.LibraryScopeSaved {
		return nil, ErrValidation
	}
	rows, total, err := f.repo.ListSkills(ctx, uid, libraryFilter(q))
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	items := make([]dto_open_api.SkillDTO, 0, len(rows))
	for _, s := range rows {
		dto, err := skillDTO(s)
		if err != nil {
			return nil, err
		}
		items = append(items, dto)
	}
	return &dto_open_api.SkillListDTO{Items: items, Total: total, Page: q.Page, Limit: q.Limit}, nil
}

func (f *libraryFacade) GetSkill(ctx context.Context, id, userID string) (*dto_open_api.SkillDTO, error) {
	rid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return nil, err
	}
	s, err := f.repo.FindSkill(ctx, rid, uid)
	if err != nil {
		return nil, libraryRepoErr("get skill", err)
	}
	dto, err := skillDTO(*s)
	if err != nil {
		return nil, err
	}
	return &dto, nil
}

func encodeSkillLists(in SkillInput) ([]byte, []byte, error) {
	steps, err := json.Marshal(in.Steps)
	if err != nil {
		return nil, nil, err
	}
	inputs, err := json.Marshal(in.Inputs)
	return steps, inputs, err
}

func (f *libraryFacade) CreateSkill(ctx context.Context, userID string, in SkillInput) (*dto_open_api.SkillDTO, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if in, err = normalizeSkill(in); err != nil {
		return nil, err
	}
	s := &models.Skill{UserID: &uid, Name: in.Name, Description: in.Description, Icon: defaultSkillIcon, Output: in.Output, Version: 1}
	if in.ForkedFromID != nil {
		srcID, err := uuid.Parse(*in.ForkedFromID)
		if err != nil {
			return nil, ErrValidation
		}
		src, err := f.repo.FindSkill(ctx, srcID, uid)
		if err != nil {
			return nil, libraryRepoErr("find fork source", err)
		}
		s.ForkedFromID, s.Icon = &src.ID, src.Icon
	}
	n, err := f.repo.CountOwnSkills(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("create skill: %w", err)
	}
	if n >= maxOwnSkills {
		return nil, ErrLibraryLimit
	}
	if s.Steps, s.Inputs, err = encodeSkillLists(in); err != nil {
		return nil, fmt.Errorf("encode skill: %w", err)
	}
	if err := f.repo.CreateSkill(ctx, s); err != nil {
		return nil, fmt.Errorf("create skill: %w", err)
	}
	dto, err := skillDTO(*s)
	if err != nil {
		return nil, err
	}
	return &dto, nil
}

func (f *libraryFacade) ownedSkill(ctx context.Context, rid, uid uuid.UUID) error {
	s, err := f.repo.FindSkill(ctx, rid, uid)
	if err != nil {
		return libraryRepoErr("find skill", err)
	}
	if s.UserID == nil {
		return ErrBuiltInReadOnly
	}
	return nil
}

func (f *libraryFacade) UpdateSkill(ctx context.Context, id, userID string, in SkillInput) (*dto_open_api.SkillDTO, error) {
	rid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return nil, err
	}
	if in, err = normalizeSkill(in); err != nil {
		return nil, err
	}
	if err := f.ownedSkill(ctx, rid, uid); err != nil {
		return nil, err
	}
	steps, inputs, err := encodeSkillLists(in)
	if err != nil {
		return nil, fmt.Errorf("encode skill: %w", err)
	}
	fields := repository_open_api.SkillFields{Name: in.Name, Description: in.Description, Steps: steps, Inputs: inputs, Output: in.Output}
	if err := f.repo.UpdateSkill(ctx, rid, uid, fields); err != nil {
		return nil, libraryRepoErr("update skill", err)
	}
	return f.GetSkill(ctx, id, userID)
}

func (f *libraryFacade) DeleteSkill(ctx context.Context, id, userID string) error {
	rid, uid, err := libraryIDs(id, userID)
	if err != nil {
		return err
	}
	if err := f.ownedSkill(ctx, rid, uid); err != nil {
		return err
	}
	return libraryRepoErr("delete skill", f.repo.DeleteSkill(ctx, rid, uid))
}
