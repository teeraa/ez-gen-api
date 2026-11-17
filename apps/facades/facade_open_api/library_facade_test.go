package facade_open_api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/google/uuid"
)

// fakeLibraryRepo keeps rows in memory and applies the same visibility rule as Postgres:
// built-ins (nil UserID) are visible to everyone, own rows only to their owner.
type fakeLibraryRepo struct {
	prompts   map[uuid.UUID]*models.PromptTemplate
	skills    map[uuid.UUID]*models.Skill
	bookmarks map[[2]uuid.UUID]bool
}

func newFakeLibraryRepo() *fakeLibraryRepo {
	return &fakeLibraryRepo{prompts: map[uuid.UUID]*models.PromptTemplate{}, skills: map[uuid.UUID]*models.Skill{}, bookmarks: map[[2]uuid.UUID]bool{}}
}

func libVisible(owner *uuid.UUID, user uuid.UUID) bool { return owner == nil || *owner == user }

func (r *fakeLibraryRepo) ListPrompts(_ context.Context, uid uuid.UUID, f repository_open_api.LibraryFilter) ([]repository_open_api.PromptRow, int64, error) {
	var out []repository_open_api.PromptRow
	for _, p := range r.prompts {
		if !libVisible(p.UserID, uid) || (f.Scope == "mine" && p.UserID == nil) || (f.Scope == "builtin" && p.UserID != nil) {
			continue
		}
		b := r.bookmarks[[2]uuid.UUID{uid, p.ID}]
		if f.Scope == "saved" && !b {
			continue
		}
		out = append(out, repository_open_api.PromptRow{PromptTemplate: *p, Bookmarked: b})
	}
	return out, int64(len(out)), nil
}

func (r *fakeLibraryRepo) FindPrompt(_ context.Context, id, uid uuid.UUID) (*repository_open_api.PromptRow, error) {
	p, ok := r.prompts[id]
	if !ok || !libVisible(p.UserID, uid) {
		return nil, repository_open_api.ErrNotFound
	}
	return &repository_open_api.PromptRow{PromptTemplate: *p, Bookmarked: r.bookmarks[[2]uuid.UUID{uid, id}]}, nil
}

func (r *fakeLibraryRepo) CountOwnPrompts(_ context.Context, uid uuid.UUID) (int64, error) {
	var n int64
	for _, p := range r.prompts {
		if p.UserID != nil && *p.UserID == uid {
			n++
		}
	}
	return n, nil
}

func (r *fakeLibraryRepo) CreatePrompt(_ context.Context, p *models.PromptTemplate) error {
	p.ID, p.CreatedAt, p.UpdatedAt = uuid.New(), time.Now(), time.Now()
	cp := *p
	r.prompts[p.ID] = &cp
	return nil
}

func (r *fakeLibraryRepo) UpdatePrompt(_ context.Context, id, uid uuid.UUID, f repository_open_api.PromptFields) error {
	p, ok := r.prompts[id]
	if !ok || p.UserID == nil || *p.UserID != uid {
		return repository_open_api.ErrNotFound
	}
	p.Title, p.Category, p.Body = f.Title, f.Category, f.Body
	return nil
}

func (r *fakeLibraryRepo) DeletePrompt(_ context.Context, id, uid uuid.UUID) error {
	p, ok := r.prompts[id]
	if !ok || p.UserID == nil || *p.UserID != uid {
		return repository_open_api.ErrNotFound
	}
	delete(r.prompts, id)
	return nil
}

func (r *fakeLibraryRepo) SetBookmark(_ context.Context, uid, pid uuid.UUID, on bool) error {
	if on {
		r.bookmarks[[2]uuid.UUID{uid, pid}] = true
	} else {
		delete(r.bookmarks, [2]uuid.UUID{uid, pid})
	}
	return nil
}

func (r *fakeLibraryRepo) ListSkills(_ context.Context, uid uuid.UUID, _ repository_open_api.LibraryFilter) ([]models.Skill, int64, error) {
	var out []models.Skill
	for _, s := range r.skills {
		if libVisible(s.UserID, uid) {
			out = append(out, *s)
		}
	}
	return out, int64(len(out)), nil
}

func (r *fakeLibraryRepo) FindSkill(_ context.Context, id, uid uuid.UUID) (*models.Skill, error) {
	s, ok := r.skills[id]
	if !ok || !libVisible(s.UserID, uid) {
		return nil, repository_open_api.ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *fakeLibraryRepo) CountOwnSkills(_ context.Context, uid uuid.UUID) (int64, error) {
	var n int64
	for _, s := range r.skills {
		if s.UserID != nil && *s.UserID == uid {
			n++
		}
	}
	return n, nil
}

func (r *fakeLibraryRepo) CreateSkill(_ context.Context, s *models.Skill) error {
	s.ID, s.CreatedAt, s.UpdatedAt = uuid.New(), time.Now(), time.Now()
	cp := *s
	r.skills[s.ID] = &cp
	return nil
}

func (r *fakeLibraryRepo) UpdateSkill(_ context.Context, id, uid uuid.UUID, f repository_open_api.SkillFields) error {
	s, ok := r.skills[id]
	if !ok || s.UserID == nil || *s.UserID != uid {
		return repository_open_api.ErrNotFound
	}
	s.Name, s.Description, s.Steps, s.Inputs, s.Output = f.Name, f.Description, f.Steps, f.Inputs, f.Output
	s.Version++
	return nil
}

func (r *fakeLibraryRepo) DeleteSkill(_ context.Context, id, uid uuid.UUID) error {
	s, ok := r.skills[id]
	if !ok || s.UserID == nil || *s.UserID != uid {
		return repository_open_api.ErrNotFound
	}
	delete(r.skills, id)
	return nil
}

func TestLibraryPromptCRUDAndOwnership(t *testing.T) {
	ctx := context.Background()
	repo := newFakeLibraryRepo()
	builtin := &models.PromptTemplate{Base: models.Base{ID: uuid.New()}, Title: "built-in", Category: "video", Body: "Cinematic {x}"}
	repo.prompts[builtin.ID] = builtin
	f := NewLibraryFacade(repo)
	alice, bob := uuid.NewString(), uuid.NewString()

	if _, err := f.CreatePrompt(ctx, alice, PromptInput{Title: " ก ", Category: "video", Body: "short"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("one-rune title after trim: err=%v, want ErrValidation", err)
	}
	p, err := f.CreatePrompt(ctx, alice, PromptInput{Title: "  ช็อตอาหาร ", Category: "image", Body: "Top-down {เมนู}"})
	if err != nil || p.Title != "ช็อตอาหาร" || p.BuiltIn {
		t.Fatalf("create: %+v err=%v", p, err)
	}

	if _, err := f.GetPrompt(ctx, p.ID, bob); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another user's prompt must be hidden: err=%v", err)
	}
	if _, err := f.GetPrompt(ctx, "not-a-uuid", alice); !errors.Is(err, ErrNotFound) {
		t.Fatalf("malformed id: err=%v", err)
	}
	if _, err := f.UpdatePrompt(ctx, builtin.ID.String(), alice, PromptInput{Title: "hack", Category: "video", Body: "hello world"}); !errors.Is(err, ErrBuiltInReadOnly) {
		t.Fatalf("update built-in: err=%v", err)
	}
	if err := f.DeletePrompt(ctx, builtin.ID.String(), alice); !errors.Is(err, ErrBuiltInReadOnly) {
		t.Fatalf("delete built-in: err=%v", err)
	}

	up, err := f.UpdatePrompt(ctx, p.ID, alice, PromptInput{Title: "ใหม่ ๆ", Category: "product", Body: "Product {สินค้า}"})
	if err != nil || up.Category != "product" {
		t.Fatalf("update own: %+v err=%v", up, err)
	}

	b, err := f.SetPromptBookmark(ctx, builtin.ID.String(), bob, true)
	if err != nil || !b.Bookmarked {
		t.Fatalf("bookmark built-in: %+v err=%v", b, err)
	}
	if _, err := f.SetPromptBookmark(ctx, p.ID, bob, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bookmark someone else's prompt: err=%v", err)
	}
	saved, _ := f.ListPrompts(ctx, bob, LibraryQuery{Scope: "saved", Page: 1, Limit: 20})
	if saved.Total != 1 || saved.Items[0].ID != builtin.ID.String() {
		t.Fatalf("saved list: %+v", saved)
	}

	if err := f.DeletePrompt(ctx, p.ID, alice); err != nil {
		t.Fatalf("delete own: %v", err)
	}
	if err := f.DeletePrompt(ctx, p.ID, alice); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: err=%v", err)
	}
}

func TestLibraryPromptLimit(t *testing.T) {
	repo := newFakeLibraryRepo()
	alice := uuid.New()
	for i := 0; i < maxOwnPrompts; i++ {
		id := uuid.New()
		repo.prompts[id] = &models.PromptTemplate{Base: models.Base{ID: id}, UserID: &alice}
	}
	_, err := NewLibraryFacade(repo).CreatePrompt(context.Background(), alice.String(), PromptInput{Title: "one more", Category: "video", Body: "hello world"})
	if !errors.Is(err, ErrLibraryLimit) {
		t.Fatalf("err=%v, want ErrLibraryLimit", err)
	}
}

func TestLibrarySkillForkNormalizeAndVersion(t *testing.T) {
	ctx := context.Background()
	repo := newFakeLibraryRepo()
	builtin := &models.Skill{Base: models.Base{ID: uuid.New()}, Name: "โฆษณา", Icon: "megaphone", Steps: []byte(`["a"]`), Inputs: []byte(`["product"]`), Output: "video_9_16", Version: 1}
	repo.skills[builtin.ID] = builtin
	f := NewLibraryFacade(repo)
	alice := uuid.NewString()

	if _, err := f.CreateSkill(ctx, alice, SkillInput{Name: "ok name", Steps: []string{"  ", ""}, Output: "storyboard"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank steps: err=%v", err)
	}

	src := builtin.ID.String()
	s, err := f.CreateSkill(ctx, alice, SkillInput{
		Name: "โฆษณาร้านกาแฟ", Steps: []string{" เขียนบท ", "", "สร้างภาพ"}, Inputs: []string{"product", "voice", "product"},
		Output: "video_9_16", ForkedFromID: &src,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Icon != "megaphone" || s.ForkedFromID == nil || *s.ForkedFromID != src || s.BuiltIn {
		t.Fatalf("fork metadata: %+v", s)
	}
	if len(s.Steps) != 2 || s.Steps[0] != "เขียนบท" || len(s.Inputs) != 2 {
		t.Fatalf("normalized lists: steps=%q inputs=%q", s.Steps, s.Inputs)
	}

	up, err := f.UpdateSkill(ctx, s.ID, alice, SkillInput{Name: "โฆษณาร้านชา", Steps: []string{"x"}, Output: "image_set"})
	if err != nil || up.Version != 2 || up.Output != "image_set" || len(up.Inputs) != 0 {
		t.Fatalf("update: %+v err=%v", up, err)
	}
	if _, err := f.UpdateSkill(ctx, src, alice, SkillInput{Name: "nope", Steps: []string{"x"}, Output: "image_set"}); !errors.Is(err, ErrBuiltInReadOnly) {
		t.Fatalf("update built-in: err=%v", err)
	}

	missing := uuid.NewString()
	if _, err := f.CreateSkill(ctx, alice, SkillInput{Name: "fork", Steps: []string{"x"}, Output: "image_set", ForkedFromID: &missing}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("fork from missing: err=%v", err)
	}
	if _, err := f.ListSkills(ctx, alice, LibraryQuery{Scope: "saved", Page: 1, Limit: 20}); !errors.Is(err, ErrValidation) {
		t.Fatalf("saved scope on skills: err=%v", err)
	}
	if err := f.DeleteSkill(ctx, s.ID, uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete by stranger: err=%v", err)
	}
	if err := f.DeleteSkill(ctx, s.ID, alice); err != nil {
		t.Fatalf("delete own: %v", err)
	}
}
