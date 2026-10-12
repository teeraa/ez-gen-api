package tests

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/google/uuid"
)

type fakeCharacterRepo struct {
	repository_open_api.CharacterProfileRepository
	profiles map[uuid.UUID]models.CharacterProfile
}

func newFakeCharacterRepo(profiles ...models.CharacterProfile) *fakeCharacterRepo {
	r := &fakeCharacterRepo{profiles: make(map[uuid.UUID]models.CharacterProfile)}
	for _, p := range profiles {
		r.profiles[p.ID] = p
	}
	return r
}

func (r *fakeCharacterRepo) FindByIDs(_ context.Context, ids []uuid.UUID, userID string) ([]models.CharacterProfile, error) {
	var out []models.CharacterProfile
	for _, id := range ids {
		if p, ok := r.profiles[id]; ok && p.UserID.String() == userID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (r *fakeSceneRepo) SetCharacters(_ context.Context, sceneID uuid.UUID, ids []uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.scenes[sceneID.String()]
	s.Characters = nil
	for i, id := range ids {
		s.Characters = append(s.Characters, models.SceneCharacter{SceneID: sceneID, CharacterProfileID: id, SortOrder: i})
	}
	s.CharacterProfileID = nil
	if len(ids) > 0 {
		first := ids[0]
		s.CharacterProfileID = &first
	}
	return nil
}

func (r *fakeSceneRepo) Update(_ context.Context, scene *models.Scene) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := *scene
	r.scenes[scene.ID.String()] = &c
	return nil
}

func newCharacter(userID uuid.UUID, name, appearance string) models.CharacterProfile {
	p := models.CharacterProfile{UserID: userID, Name: name, Appearance: &appearance, Version: 1}
	p.ID = uuid.New()
	return p
}

func newDNAFacade(chars *fakeCharacterRepo, scenes *fakeSceneRepo) facade_open_api.DNAFacade {
	return facade_open_api.NewDNAFacade(nil, chars, nil, scenes)
}

func sceneCharacterOrder(s *models.Scene) []uuid.UUID {
	var ids []uuid.UUID
	for _, c := range s.Characters {
		ids = append(ids, c.CharacterProfileID)
	}
	return ids
}

func TestCreateScene_MultipleCharactersKeepOrderAndMirrorFirst(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	a, b := newCharacter(userID, "Elena", "tall"), newCharacter(userID, "Somchai", "farmer")
	scenes := &fakeSceneRepo{scenes: map[string]*models.Scene{}}
	f := newDNAFacade(newFakeCharacterRepo(a, b), scenes)

	_, err := f.CreateScene(t.Context(), userID.String(), facade_open_api.CreateSceneInput{
		Title:               "rice",
		CharacterProfileIDs: []string{b.ID.String(), a.ID.String()},
	})
	if err != nil {
		t.Fatalf("create scene: %v", err)
	}
	if len(scenes.scenes) != 1 {
		t.Fatalf("expected one scene, got %d", len(scenes.scenes))
	}
	for _, s := range scenes.scenes {
		got := sceneCharacterOrder(s)
		if len(got) != 2 || got[0] != b.ID || got[1] != a.ID {
			t.Errorf("character order = %v, want [Somchai, Elena]", got)
		}
		if s.CharacterProfileID == nil || *s.CharacterProfileID != b.ID {
			t.Errorf("legacy characterProfileId should mirror the first character")
		}
	}
}

func TestCreateScene_RejectsInvalidCharacterLists(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	mine := newCharacter(userID, "Elena", "tall")
	theirs := newCharacter(uuid.New(), "Other", "x")
	ids := func(n int) []string {
		var out []string
		for range n {
			out = append(out, newCharacter(userID, "x", "y").ID.String())
		}
		return out
	}
	cases := []struct {
		name string
		ids  []string
		want error
	}{
		{"more than three", ids(4), facade_open_api.ErrValidation},
		{"duplicate", []string{mine.ID.String(), mine.ID.String()}, facade_open_api.ErrValidation},
		{"another user's character", []string{mine.ID.String(), theirs.ID.String()}, facade_open_api.ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scenes := &fakeSceneRepo{scenes: map[string]*models.Scene{}}
			f := newDNAFacade(newFakeCharacterRepo(mine, theirs), scenes)
			_, err := f.CreateScene(t.Context(), userID.String(), facade_open_api.CreateSceneInput{CharacterProfileIDs: tc.ids})
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
			if len(scenes.scenes) != 0 {
				t.Error("scene must not be created when characters are invalid")
			}
		})
	}
}

func TestUpdateScene_CharacterReplacement(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	a, b, c := newCharacter(userID, "A", "a"), newCharacter(userID, "B", "b"), newCharacter(userID, "C", "c")
	empty := ""
	legacy := c.ID.String()
	cases := []struct {
		name   string
		in     facade_open_api.UpdateSceneInput
		expect []uuid.UUID
	}{
		{"list replaces", facade_open_api.UpdateSceneInput{CharacterProfileIDs: []string{b.ID.String(), c.ID.String()}}, []uuid.UUID{b.ID, c.ID}},
		{"empty list clears", facade_open_api.UpdateSceneInput{CharacterProfileIDs: []string{}}, nil},
		{"legacy field replaces with one", facade_open_api.UpdateSceneInput{CharacterProfileID: &legacy}, []uuid.UUID{c.ID}},
		{"legacy empty clears", facade_open_api.UpdateSceneInput{CharacterProfileID: &empty}, nil},
		{"absent keeps", facade_open_api.UpdateSceneInput{}, []uuid.UUID{a.ID, b.ID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &models.Scene{UserID: userID}
			s.ID = uuid.New()
			scenes := &fakeSceneRepo{scenes: map[string]*models.Scene{s.ID.String(): s}}
			_ = scenes.SetCharacters(t.Context(), s.ID, []uuid.UUID{a.ID, b.ID})
			f := newDNAFacade(newFakeCharacterRepo(a, b, c), scenes)

			if _, err := f.UpdateScene(t.Context(), s.ID.String(), userID.String(), tc.in); err != nil {
				t.Fatalf("update: %v", err)
			}
			got := sceneCharacterOrder(scenes.scenes[s.ID.String()])
			if len(got) != len(tc.expect) {
				t.Fatalf("characters = %v, want %v", got, tc.expect)
			}
			for i := range got {
				if got[i] != tc.expect[i] {
					t.Errorf("characters = %v, want %v", got, tc.expect)
				}
			}
		})
	}
}

func TestGenerateForScene_UsesSceneCharactersInOrder(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	a, b := newCharacter(userID, "Elena", "tall woman"), newCharacter(userID, "Somchai", "elderly farmer")
	s := &models.Scene{UserID: userID}
	s.ID = uuid.New()
	scenes := &fakeSceneRepo{scenes: map[string]*models.Scene{s.ID.String(): s}}
	_ = scenes.SetCharacters(t.Context(), s.ID, []uuid.UUID{a.ID, b.ID})

	jobs := newFakeJobRepo()
	ark := modelark.NewMockClient()
	registry, _ := modelark.NewRegistry("seedance-1-0-pro-250528")
	f := facade_open_api.NewJobFacade(jobs, nil, newFakeCharacterRepo(a, b), nil, scenes, nil, ark, registry,
		storage.NewMemoryClient(), &fakeFetcher{}, 3)

	sceneID := s.ID.String()
	if _, err := f.CreateJob(t.Context(), userID.String(), facade_open_api.CreateJobInput{
		Prompt: "Elena hands rice to Somchai", Mode: "text", ModelID: "violet-classic",
		AspectRatio: "16:9", DurationSeconds: 5, Quality: "standard", SceneID: &sceneID,
	}); err != nil {
		t.Fatalf("create job: %v", err)
	}
	prompt := ark.CreatedRequests[0].Content[0].Text
	if !strings.Contains(prompt, "Elena: tall woman") || !strings.Contains(prompt, "Somchai: elderly farmer") {
		t.Errorf("prompt missing scene characters: %q", prompt)
	}
	for _, j := range jobs.jobs {
		if j.CharacterProfileID == nil || *j.CharacterProfileID != a.ID {
			t.Error("job.character_profile_id should be the first character")
		}
	}
}

func TestCreateJob_ExplicitCharacterOfAnotherUserIsNotFound(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	theirs := newCharacter(uuid.New(), "Other", "x")
	ark := modelark.NewMockClient()
	registry, _ := modelark.NewRegistry("")
	f := facade_open_api.NewJobFacade(newFakeJobRepo(), nil, newFakeCharacterRepo(theirs), nil, nil, nil, ark, registry,
		storage.NewMemoryClient(), &fakeFetcher{}, 3)

	_, err := f.CreateJob(t.Context(), userID.String(), facade_open_api.CreateJobInput{
		Prompt: "x", Mode: "text", ModelID: "violet-classic", AspectRatio: "16:9", DurationSeconds: 5,
		Quality: "standard", CharacterProfileIDs: []string{theirs.ID.String()},
	})
	if !errors.Is(err, facade_open_api.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if len(ark.CreatedRequests) != 0 {
		t.Error("no provider task may be created")
	}
}

func TestContinueScene_CopiesAllCharacters(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	a, b := newCharacter(userID, "Elena", "tall"), newCharacter(userID, "Somchai", "farmer")
	prev := &models.Scene{UserID: userID}
	prev.ID = uuid.New()
	scenes := &fakeSceneRepo{scenes: map[string]*models.Scene{prev.ID.String(): prev}}
	_ = scenes.SetCharacters(t.Context(), prev.ID, []uuid.UUID{a.ID, b.ID})

	ark := modelark.NewMockClient()
	registry, _ := modelark.NewRegistry("")
	f := facade_open_api.NewJobFacade(newFakeJobRepo(), nil, newFakeCharacterRepo(a, b), nil, scenes, nil, ark, registry,
		storage.NewMemoryClient(), &fakeFetcher{}, 3)

	if _, err := f.ContinueScene(t.Context(), prev.ID.String(), userID.String(), facade_open_api.ContinueSceneInput{
		ActionOverride: "they walk home", ModelID: "violet-classic", AspectRatio: "16:9",
		DurationSeconds: 5, Quality: "standard",
	}); err != nil {
		t.Fatalf("continue: %v", err)
	}
	for id, s := range scenes.scenes {
		if id == prev.ID.String() {
			continue
		}
		got := sceneCharacterOrder(s)
		if len(got) != 2 || got[0] != a.ID || got[1] != b.ID {
			t.Errorf("continued scene characters = %v", got)
		}
	}
	if p := ark.CreatedRequests[0].Content[0].Text; !strings.Contains(p, "Somchai:") {
		t.Errorf("continued job prompt missing second character: %q", p)
	}
}
