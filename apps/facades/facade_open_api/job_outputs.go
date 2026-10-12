package facade_open_api

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/ez-gen/ez-gen-api/apps/services/storage"
)

// BytePlus signs output URLs for 24h; past that a download can never succeed.
const providerOutputLifetime = 24 * time.Hour

const signedURLTTL = time.Hour

type mediaStorage interface {
	Upload(ctx context.Context, key string, data []byte, contentType string) error
	SignedURL(ctx context.Context, key string, ttl time.Duration) (string, time.Time, error)
}

type modelRegistry interface {
	Lookup(internalID string) (modelark.ModelConfig, bool)
}

type mediaFetcher interface {
	Fetch(ctx context.Context, rawURL string) ([]byte, string, error)
}

// persistOutputs copies provider output into application storage. The thumbnail is
// best-effort: losing it must not block the video from being saved.
func (f *jobFacade) persistOutputs(ctx context.Context, job *models.VideoGenerationJob, videoURL, thumbURL string) (string, *string, error) {
	if videoURL == "" {
		return "", nil, fmt.Errorf("provider returned no video url")
	}
	userID, jobID := job.UserID.String(), job.ID.String()

	data, contentType, err := f.fetcher.Fetch(ctx, videoURL)
	if err != nil {
		return "", nil, fmt.Errorf("download video: %w", err)
	}
	if !strings.HasPrefix(contentType, "video/") {
		contentType = "video/mp4"
	}
	mediaKey := storage.KeyForOutput(userID, jobID, storage.ExtForContentType(contentType, "mp4"))
	if err := f.storage.Upload(ctx, mediaKey, data, contentType); err != nil {
		return "", nil, fmt.Errorf("upload video: %w", err)
	}

	if thumbURL == "" {
		return mediaKey, nil, nil
	}
	thumb, thumbType, err := f.fetcher.Fetch(ctx, thumbURL)
	if err != nil {
		log.Printf("[storage] job=%s thumbnail download skipped: %v", jobID, err)
		return mediaKey, nil, nil
	}
	if !strings.HasPrefix(thumbType, "image/") {
		thumbType = "image/png"
	}
	thumbKey := storage.KeyForThumbnail(userID, jobID, storage.ExtForContentType(thumbType, "png"))
	if err := f.storage.Upload(ctx, thumbKey, thumb, thumbType); err != nil {
		log.Printf("[storage] job=%s thumbnail upload skipped: %v", jobID, err)
		return mediaKey, nil, nil
	}
	return mediaKey, &thumbKey, nil
}

func (f *jobFacade) BackfillOutputs(ctx context.Context, limit int) (int, error) {
	jobs, err := f.jobs.ListUnpersistedOutputs(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("backfill outputs: %w", err)
	}
	stored := 0
	for i := range jobs {
		job := &jobs[i]
		thumbURL := ""
		if job.OutputThumbKey != nil && isProviderURL(*job.OutputThumbKey) {
			thumbURL = *job.OutputThumbKey
		}
		mediaKey, thumbKey, err := f.persistOutputs(ctx, job, *job.OutputMediaKey, thumbURL)
		if err != nil {
			log.Printf("[storage] backfill job=%s failed: %v", job.ID, err)
			continue
		}
		if err := f.jobs.UpdateOutputKeys(ctx, job.ID.String(), mediaKey, thumbKey); err != nil {
			log.Printf("[storage] backfill job=%s update keys: %v", job.ID, err)
			continue
		}
		stored++
	}
	return stored, nil
}

func (f *jobFacade) attachResult(ctx context.Context, job *models.VideoGenerationJob, dto *dto_open_api.JobDTO) {
	if job.Status != models.JobStatusSucceeded || job.OutputMediaKey == nil {
		return
	}
	mediaURL, expiresAt, err := f.resolveMediaURL(ctx, *job.OutputMediaKey)
	if err != nil {
		log.Printf("[storage] job=%s sign media url: %v", job.ID, err)
		return
	}
	dto.Result = &dto_open_api.JobResult{
		MediaURL:  mediaURL,
		MediaType: strValOrDefault(job.OutputMediaType, "video"),
	}
	if !expiresAt.IsZero() {
		s := expiresAt.UTC().Format(time.RFC3339)
		dto.Result.ExpiresAt = &s
	}
	if job.OutputThumbKey != nil {
		if thumbURL, _, err := f.resolveMediaURL(ctx, *job.OutputThumbKey); err == nil {
			dto.Result.ThumbnailURL = thumbURL
		}
	}
}

// resolveMediaURL passes through provider URLs on rows written before outputs were
// persisted, so those jobs stay playable until the backfill moves them.
func (f *jobFacade) resolveMediaURL(ctx context.Context, key string) (string, time.Time, error) {
	return resolveMediaURL(ctx, f.storage, key)
}

func resolveMediaURL(ctx context.Context, store mediaStorage, key string) (string, time.Time, error) {
	if isProviderURL(key) {
		return key, time.Time{}, nil
	}
	return store.SignedURL(ctx, key, signedURLTTL)
}

func isProviderURL(key string) bool {
	return strings.HasPrefix(key, "http://") || strings.HasPrefix(key, "https://")
}

func frameMimeType(key string) string {
	path := key
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	switch {
	case strings.HasSuffix(path, ".jpg"), strings.HasSuffix(path, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(path, ".webp"):
		return "image/webp"
	default:
		return "image/png"
	}
}
