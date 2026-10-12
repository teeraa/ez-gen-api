package facade_open_api

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/google/uuid"
)

// Signing is one Supabase round trip per uncached URL, so a 50-scene storyboard is
// signed in parallel with a bound to stay polite to the storage API.
const previewSignConcurrency = 8

// attachScenePreviews fills dtos[i].Preview for scenes[i]. Failures are logged and leave
// the preview null: a missing thumbnail must never break the storyboard.
func (f *dnaFacade) attachScenePreviews(ctx context.Context, userID string, scenes []models.Scene, dtos []dto_open_api.SceneDTO) {
	if len(scenes) == 0 || f.jobs == nil || f.storage == nil {
		return
	}
	previews, err := f.previewJobs(ctx, userID, scenes)
	if err != nil {
		log.Printf("[storyboard] load preview jobs: %v", err)
		return
	}

	sem := make(chan struct{}, previewSignConcurrency)
	var wg sync.WaitGroup
	for i := range scenes {
		p, ok := previews[scenes[i].ID]
		if !ok {
			continue
		}
		wg.Add(1)
		go func(i int, p previewJob) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			dtos[i].Preview = f.signPreview(ctx, p)
		}(i, p)
	}
	wg.Wait()
}

type previewJob struct {
	job      models.VideoGenerationJob
	selected bool
}

// previewJobs picks the selected job when it has a stored output, otherwise the scene's
// latest succeeded job. Both queries filter by user, so a selected_job_id pointing at
// someone else's job yields no preview.
func (f *dnaFacade) previewJobs(ctx context.Context, userID string, scenes []models.Scene) (map[uuid.UUID]previewJob, error) {
	var selectedIDs []uuid.UUID
	for _, s := range scenes {
		if s.SelectedJobID != nil {
			selectedIDs = append(selectedIDs, *s.SelectedJobID)
		}
	}
	selected, err := f.jobs.ListByIDs(ctx, selectedIDs, userID)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]models.VideoGenerationJob, len(selected))
	for _, j := range selected {
		byID[j.ID] = j
	}

	out := make(map[uuid.UUID]previewJob, len(scenes))
	var missing []uuid.UUID
	for _, s := range scenes {
		if s.SelectedJobID != nil {
			if j, ok := byID[*s.SelectedJobID]; ok && hasPreview(&j) {
				out[s.ID] = previewJob{job: j, selected: true}
				continue
			}
		}
		missing = append(missing, s.ID)
	}

	latest, err := f.jobs.LatestSucceededByScenes(ctx, missing, userID)
	if err != nil {
		return nil, err
	}
	for _, j := range latest {
		if j.SceneID != nil && hasPreview(&j) {
			out[*j.SceneID] = previewJob{job: j}
		}
	}
	return out, nil
}

func hasPreview(j *models.VideoGenerationJob) bool {
	return j.Status == models.JobStatusSucceeded && j.OutputMediaKey != nil
}

func (f *dnaFacade) signPreview(ctx context.Context, p previewJob) *dto_open_api.ScenePreviewDTO {
	mediaURL, expiresAt, err := resolveMediaURL(ctx, f.storage, *p.job.OutputMediaKey)
	if err != nil {
		log.Printf("[storyboard] job=%s sign media: %v", p.job.ID, err)
		return nil
	}
	dto := &dto_open_api.ScenePreviewDTO{
		JobID:      p.job.ID.String(),
		IsSelected: p.selected,
		MediaURL:   mediaURL,
		MediaType:  strValOrDefault(p.job.OutputMediaType, "video"),
	}
	if !expiresAt.IsZero() {
		s := expiresAt.UTC().Format(time.RFC3339)
		dto.ExpiresAt = &s
	}
	if p.job.OutputThumbKey != nil {
		if thumb, _, err := resolveMediaURL(ctx, f.storage, *p.job.OutputThumbKey); err == nil {
			dto.ThumbnailURL = &thumb
		}
	}
	return dto
}
