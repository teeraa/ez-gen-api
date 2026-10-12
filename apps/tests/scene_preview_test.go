package tests

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
	"github.com/google/uuid"
)

func (r *fakeJobRepo) ListByIDs(_ context.Context, ids []uuid.UUID, userID string) ([]models.VideoGenerationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.VideoGenerationJob
	for _, id := range ids {
		if j, ok := r.jobs[id.String()]; ok && j.UserID.String() == userID {
			out = append(out, *j)
		}
	}
	return out, nil
}

func (r *fakeJobRepo) LatestSucceededByScenes(_ context.Context, sceneIDs []uuid.UUID, userID string) ([]models.VideoGenerationJob, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	want := map[uuid.UUID]bool{}
	for _, id := range sceneIDs {
		want[id] = true
	}
	var candidates []models.VideoGenerationJob
	for _, j := range r.jobs {
		if j.SceneID != nil && want[*j.SceneID] && j.UserID.String() == userID &&
			j.Status == models.JobStatusSucceeded && j.OutputMediaKey != nil {
			candidates = append(candidates, *j)
		}
	}
	sort.Slice(candidates, func(a, b int) bool { return candidates[a].CompletedAt.After(*candidates[b].CompletedAt) })
	seen := map[uuid.UUID]bool{}
	var out []models.VideoGenerationJob
	for _, j := range candidates {
		if !seen[*j.SceneID] {
			seen[*j.SceneID] = true
			out = append(out, j)
		}
	}
	return out, nil
}

func sceneJob(userID, sceneID uuid.UUID, status models.JobStatus, completedAgo time.Duration, withMedia bool) *models.VideoGenerationJob {
	j := &models.VideoGenerationJob{UserID: userID, SceneID: &sceneID, Status: status}
	j.ID = uuid.New()
	done := time.Now().Add(-completedAgo)
	j.CompletedAt = &done
	if withMedia {
		key := "outputs/" + userID.String() + "/" + j.ID.String() + ".mp4"
		thumb := "thumbs/" + userID.String() + "/" + j.ID.String() + ".png"
		j.OutputMediaKey, j.OutputThumbKey = &key, &thumb
	}
	return j
}

func TestListScenes_PreviewSelectedThenLatestThenNone(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	mk := func() *models.Scene {
		s := &models.Scene{UserID: userID}
		s.ID = uuid.New()
		return s
	}
	selectedScene, fallbackScene, emptyScene, foreignScene := mk(), mk(), mk(), mk()

	chosen := sceneJob(userID, selectedScene.ID, models.JobStatusSucceeded, 2*time.Hour, true)
	newer := sceneJob(userID, selectedScene.ID, models.JobStatusSucceeded, time.Minute, true)
	selectedScene.SelectedJobID = &chosen.ID

	oldOK := sceneJob(userID, fallbackScene.ID, models.JobStatusSucceeded, time.Hour, true)
	latestOK := sceneJob(userID, fallbackScene.ID, models.JobStatusSucceeded, 10*time.Minute, true)
	failed := sceneJob(userID, fallbackScene.ID, models.JobStatusFailed, time.Minute, false)
	fallbackScene.SelectedJobID = &failed.ID

	running := sceneJob(userID, emptyScene.ID, models.JobStatusProcessing, 0, false)

	stranger := sceneJob(uuid.New(), foreignScene.ID, models.JobStatusSucceeded, time.Minute, true)
	foreignScene.SelectedJobID = &stranger.ID

	jobs := newFakeJobRepo(chosen, newer, oldOK, latestOK, failed, running, stranger)
	scenes := []*models.Scene{selectedScene, fallbackScene, emptyScene, foreignScene}
	f := newPreviewFacade(scenes, jobs)

	got := map[string]*struct {
		jobID    string
		selected bool
		thumb    bool
	}{}
	list, err := f.ListScenes(t.Context(), userID.String(), 1, 20)
	if err != nil {
		t.Fatalf("list scenes: %v", err)
	}
	for _, item := range list.Items {
		if item.Preview == nil {
			got[item.ID] = nil
			continue
		}
		got[item.ID] = &struct {
			jobID    string
			selected bool
			thumb    bool
		}{item.Preview.JobID, item.Preview.IsSelected, item.Preview.ThumbnailURL != nil}
		if item.Preview.MediaURL != "https://storage.test/signed/outputs/"+userID.String()+"/"+item.Preview.JobID+".mp4" {
			t.Errorf("scene %s mediaUrl = %q", item.ID, item.Preview.MediaURL)
		}
		if item.Preview.ExpiresAt == nil {
			t.Errorf("scene %s preview has no expiresAt", item.ID)
		}
	}

	if p := got[selectedScene.ID.String()]; p == nil || p.jobID != chosen.ID.String() || !p.selected || !p.thumb {
		t.Errorf("selected scene preview = %+v, want selected job %s", p, chosen.ID)
	}
	if p := got[fallbackScene.ID.String()]; p == nil || p.jobID != latestOK.ID.String() || p.selected {
		t.Errorf("fallback scene preview = %+v, want latest succeeded %s", p, latestOK.ID)
	}
	if p := got[emptyScene.ID.String()]; p != nil {
		t.Errorf("scene without output should have null preview, got %+v", p)
	}
	if p := got[foreignScene.ID.String()]; p != nil {
		t.Errorf("another user's job must not be previewed, got %+v", p)
	}
}

func TestGetScene_IncludesPreview(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	s := &models.Scene{UserID: userID}
	s.ID = uuid.New()
	j := sceneJob(userID, s.ID, models.JobStatusSucceeded, time.Minute, true)
	s.SelectedJobID = &j.ID
	f := newPreviewFacade([]*models.Scene{s}, newFakeJobRepo(j))

	dto, err := f.GetScene(t.Context(), s.ID.String(), userID.String())
	if err != nil {
		t.Fatalf("get scene: %v", err)
	}
	if dto.Preview == nil || dto.Preview.JobID != j.ID.String() {
		t.Errorf("preview = %+v", dto.Preview)
	}
}

func TestSelectSceneOutput_RejectsAnotherUsersJob(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	s := &models.Scene{UserID: userID}
	s.ID = uuid.New()
	foreign := sceneJob(uuid.New(), s.ID, models.JobStatusSucceeded, time.Minute, true)
	f := newPreviewFacade([]*models.Scene{s}, newFakeJobRepo(foreign))

	err := f.SelectSceneOutput(t.Context(), s.ID.String(), foreign.ID.String(), userID.String())
	if err == nil {
		t.Fatal("selecting another user's job must fail")
	}
}

type listingSceneRepo struct {
	*fakeSceneRepo
	order []*models.Scene
}

func (r *listingSceneRepo) List(_ context.Context, _ string, _, _ int) ([]models.Scene, int64, error) {
	out := make([]models.Scene, len(r.order))
	for i, s := range r.order {
		out[i] = *s
	}
	return out, int64(len(out)), nil
}

func newPreviewFacade(scenes []*models.Scene, jobs *fakeJobRepo) facade_open_api.DNAFacade {
	base := &fakeSceneRepo{scenes: map[string]*models.Scene{}}
	for _, s := range scenes {
		base.scenes[s.ID.String()] = s
	}
	repo := &listingSceneRepo{fakeSceneRepo: base, order: scenes}
	return facade_open_api.NewDNAFacade(nil, newFakeCharacterRepo(), nil, repo, jobs, storage.NewMemoryClient())
}
