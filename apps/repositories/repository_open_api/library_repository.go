package repository_open_api

import (
	"context"
	"strings"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Library scopes. Built-ins (user_id IS NULL) are visible to every user; own rows only to their owner.
const (
	LibraryScopeAll     = "all"
	LibraryScopeMine    = "mine"
	LibraryScopeBuiltin = "builtin"
	LibraryScopeSaved   = "saved" // prompts only
)

type LibraryFilter struct {
	Scope    string
	Category string // prompts only; empty means any
	Query    string
	Limit    int
	Offset   int
}

type PromptRow struct {
	models.PromptTemplate
	Bookmarked bool
}

type PromptFields struct {
	Title    string
	Category string
	Body     string
}

type SkillFields struct {
	Name        string
	Description string
	Steps       []byte
	Inputs      []byte
	Output      string
}

type LibraryRepository interface {
	ListPrompts(ctx context.Context, userID uuid.UUID, f LibraryFilter) ([]PromptRow, int64, error)
	FindPrompt(ctx context.Context, id, userID uuid.UUID) (*PromptRow, error)
	CountOwnPrompts(ctx context.Context, userID uuid.UUID) (int64, error)
	CreatePrompt(ctx context.Context, p *models.PromptTemplate) error
	UpdatePrompt(ctx context.Context, id, userID uuid.UUID, f PromptFields) error
	DeletePrompt(ctx context.Context, id, userID uuid.UUID) error
	SetBookmark(ctx context.Context, userID, promptID uuid.UUID, on bool) error

	ListSkills(ctx context.Context, userID uuid.UUID, f LibraryFilter) ([]models.Skill, int64, error)
	FindSkill(ctx context.Context, id, userID uuid.UUID) (*models.Skill, error)
	CountOwnSkills(ctx context.Context, userID uuid.UUID) (int64, error)
	CreateSkill(ctx context.Context, s *models.Skill) error
	UpdateSkill(ctx context.Context, id, userID uuid.UUID, f SkillFields) error
	DeleteSkill(ctx context.Context, id, userID uuid.UUID) error
}

type libraryRepository struct{ db *gorm.DB }

func NewLibraryRepository(db *gorm.DB) LibraryRepository {
	return &libraryRepository{db: db}
}

func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// scopeClause appends the visibility/scope predicate for alias a ("p" or "s").
func scopeClause(a, scope string, userID uuid.UUID, where []string, args []any) ([]string, []any) {
	switch scope {
	case LibraryScopeMine:
		return append(where, a+".user_id = ?"), append(args, userID)
	case LibraryScopeBuiltin:
		return append(where, a+".user_id IS NULL"), args
	default:
		return append(where, "("+a+".user_id IS NULL OR "+a+".user_id = ?)"), append(args, userID)
	}
}

const promptFrom = ` FROM prompt_templates p LEFT JOIN prompt_bookmarks b ON b.prompt_id = p.id AND b.user_id = ? WHERE `

func (r *libraryRepository) ListPrompts(ctx context.Context, userID uuid.UUID, f LibraryFilter) ([]PromptRow, int64, error) {
	where := []string{"p.deleted_at IS NULL"}
	args := []any{userID} // bound to the bookmark join
	where, args = scopeClause("p", f.Scope, userID, where, args)
	if f.Scope == LibraryScopeSaved {
		where = append(where, "b.user_id IS NOT NULL")
	}
	if f.Category != "" {
		where = append(where, "p.category = ?")
		args = append(args, f.Category)
	}
	if f.Query != "" {
		where = append(where, `(p.title ILIKE ? OR p.body ILIKE ?)`)
		pat := likePattern(f.Query)
		args = append(args, pat, pat)
	}
	cond := strings.Join(where, " AND ")

	var total int64
	if err := r.db.WithContext(ctx).Raw("SELECT count(*)"+promptFrom+cond, args...).Scan(&total).Error; err != nil {
		return nil, 0, wrapErr("count prompts", err)
	}
	var rows []PromptRow
	q := "SELECT p.*, (b.user_id IS NOT NULL) AS bookmarked" + promptFrom + cond +
		" ORDER BY (p.user_id IS NULL), p.sort_order, p.created_at DESC, p.id LIMIT ? OFFSET ?"
	if err := r.db.WithContext(ctx).Raw(q, append(args, f.Limit, f.Offset)...).Scan(&rows).Error; err != nil {
		return nil, 0, wrapErr("list prompts", err)
	}
	return rows, total, nil
}

func (r *libraryRepository) FindPrompt(ctx context.Context, id, userID uuid.UUID) (*PromptRow, error) {
	var rows []PromptRow
	q := "SELECT p.*, (b.user_id IS NOT NULL) AS bookmarked" + promptFrom +
		"p.id = ? AND p.deleted_at IS NULL AND (p.user_id IS NULL OR p.user_id = ?) LIMIT 1"
	if err := r.db.WithContext(ctx).Raw(q, userID, id, userID).Scan(&rows).Error; err != nil {
		return nil, wrapErr("find prompt", err)
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

func (r *libraryRepository) CountOwnPrompts(ctx context.Context, userID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&models.PromptTemplate{}).Where("user_id = ?", userID).Count(&n).Error
	return n, wrapErr("count own prompts", err)
}

func (r *libraryRepository) CreatePrompt(ctx context.Context, p *models.PromptTemplate) error {
	return wrapErr("create prompt", r.db.WithContext(ctx).Create(p).Error)
}

func (r *libraryRepository) UpdatePrompt(ctx context.Context, id, userID uuid.UUID, f PromptFields) error {
	res := r.db.WithContext(ctx).Model(&models.PromptTemplate{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]any{"title": f.Title, "category": f.Category, "body": f.Body, "updated_at": time.Now()})
	if res.Error != nil {
		return wrapErr("update prompt", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *libraryRepository) DeletePrompt(ctx context.Context, id, userID uuid.UUID) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&models.PromptTemplate{})
	if res.Error != nil {
		return wrapErr("delete prompt", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *libraryRepository) SetBookmark(ctx context.Context, userID, promptID uuid.UUID, on bool) error {
	db := r.db.WithContext(ctx)
	if on {
		return wrapErr("bookmark prompt", db.Exec(
			"INSERT INTO prompt_bookmarks (user_id, prompt_id) VALUES (?, ?) ON CONFLICT DO NOTHING", userID, promptID).Error)
	}
	return wrapErr("unbookmark prompt", db.Exec(
		"DELETE FROM prompt_bookmarks WHERE user_id = ? AND prompt_id = ?", userID, promptID).Error)
}

func (r *libraryRepository) ListSkills(ctx context.Context, userID uuid.UUID, f LibraryFilter) ([]models.Skill, int64, error) {
	where, args := scopeClause("s", f.Scope, userID, []string{"s.deleted_at IS NULL"}, nil)
	if f.Query != "" {
		where = append(where, `(s.name ILIKE ? OR s.description ILIKE ?)`)
		pat := likePattern(f.Query)
		args = append(args, pat, pat)
	}
	cond := strings.Join(where, " AND ")

	var total int64
	if err := r.db.WithContext(ctx).Raw("SELECT count(*) FROM skills s WHERE "+cond, args...).Scan(&total).Error; err != nil {
		return nil, 0, wrapErr("count skills", err)
	}
	var rows []models.Skill
	q := "SELECT s.* FROM skills s WHERE " + cond +
		" ORDER BY (s.user_id IS NULL), s.sort_order, s.created_at DESC, s.id LIMIT ? OFFSET ?"
	if err := r.db.WithContext(ctx).Raw(q, append(args, f.Limit, f.Offset)...).Scan(&rows).Error; err != nil {
		return nil, 0, wrapErr("list skills", err)
	}
	return rows, total, nil
}

func (r *libraryRepository) FindSkill(ctx context.Context, id, userID uuid.UUID) (*models.Skill, error) {
	var s models.Skill
	err := r.db.WithContext(ctx).
		Where("id = ? AND (user_id IS NULL OR user_id = ?)", id, userID).
		First(&s).Error
	if err != nil {
		return nil, wrapErr("find skill", err)
	}
	return &s, nil
}

func (r *libraryRepository) CountOwnSkills(ctx context.Context, userID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&models.Skill{}).Where("user_id = ?", userID).Count(&n).Error
	return n, wrapErr("count own skills", err)
}

func (r *libraryRepository) CreateSkill(ctx context.Context, s *models.Skill) error {
	return wrapErr("create skill", r.db.WithContext(ctx).Create(s).Error)
}

func (r *libraryRepository) UpdateSkill(ctx context.Context, id, userID uuid.UUID, f SkillFields) error {
	res := r.db.WithContext(ctx).Model(&models.Skill{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]any{
			"name": f.Name, "description": f.Description, "steps": f.Steps, "inputs": f.Inputs,
			"output": f.Output, "version": gorm.Expr("version + 1"), "updated_at": time.Now(),
		})
	if res.Error != nil {
		return wrapErr("update skill", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *libraryRepository) DeleteSkill(ctx context.Context, id, userID uuid.UUID) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&models.Skill{})
	if res.Error != nil {
		return wrapErr("delete skill", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
