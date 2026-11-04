package facade_open_api

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
)

const (
	showcaseLimit = 24
	// The list is cached well inside the signed URL lifetime, so a URL handed out
	// at the end of a cache window still has ~45 minutes left to play.
	showcaseCacheTTL = 10 * time.Minute
	// An empty list is usually "not curated yet"; recheck soon so new items appear quickly.
	showcaseEmptyCacheTTL = 30 * time.Second
)

// ShowcaseFacade serves curated landing-page videos with short-lived signed URLs.
type ShowcaseFacade interface {
	List(ctx context.Context) ([]dto_open_api.ShowcaseItemDTO, error)
}

type showcaseStorage interface {
	SignedURL(ctx context.Context, key string, ttl time.Duration) (string, time.Time, error)
}

type showcaseFacade struct {
	repo    repository_open_api.ShowcaseRepository
	storage showcaseStorage
	now     func() time.Time

	mu       sync.Mutex
	cached   []dto_open_api.ShowcaseItemDTO
	cachedAt time.Time
	cacheTTL time.Duration
}

func NewShowcaseFacade(repo repository_open_api.ShowcaseRepository, storage showcaseStorage) ShowcaseFacade {
	return &showcaseFacade{repo: repo, storage: storage, now: time.Now}
}

func (f *showcaseFacade) List(ctx context.Context) ([]dto_open_api.ShowcaseItemDTO, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cached != nil && f.now().Sub(f.cachedAt) < f.cacheTTL {
		return f.cached, nil
	}

	rows, err := f.repo.ListPublic(ctx, showcaseLimit)
	if err != nil {
		return nil, fmt.Errorf("list showcase: %w", err)
	}
	items := make([]dto_open_api.ShowcaseItemDTO, 0, len(rows))
	for _, row := range rows {
		// Provider URLs expire within a day and would leak BytePlus links.
		if isProviderURL(row.OutputMediaKey) {
			continue
		}
		mediaURL, expiresAt, err := f.storage.SignedURL(ctx, row.OutputMediaKey, signedURLTTL)
		if err != nil {
			log.Printf("[showcase] sign media item=%s: %v", row.ID, err)
			continue
		}
		item := dto_open_api.ShowcaseItemDTO{
			ID:          row.ID.String(),
			Title:       row.Title,
			Slot:        row.Slot,
			MediaType:   strValOrDefault(row.OutputMediaType, "video"),
			Subtitle:    row.Subtitle,
			MediaURL:    mediaURL,
			AspectRatio: row.AspectRatio,
			Duration:    row.DurationSeconds,
			ExpiresAt:   expiresAt.UTC().Format(time.RFC3339),
		}
		if row.OutputThumbKey != nil && !isProviderURL(*row.OutputThumbKey) {
			if thumb, _, err := f.storage.SignedURL(ctx, *row.OutputThumbKey, signedURLTTL); err == nil {
				item.ThumbnailURL = &thumb
			}
		}
		items = append(items, item)
	}
	f.cached, f.cachedAt, f.cacheTTL = items, f.now(), showcaseCacheTTL
	if len(items) == 0 {
		f.cacheTTL = showcaseEmptyCacheTTL
	}
	return items, nil
}
