package facade_open_api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/google/uuid"
)

type fakeShowcaseRepo struct {
	rows  []repository_open_api.ShowcaseRow
	calls int
}

func (r *fakeShowcaseRepo) ListPublic(_ context.Context, _ int) ([]repository_open_api.ShowcaseRow, error) {
	r.calls++
	return r.rows, nil
}

type fakeShowcaseStorage struct{ fail map[string]bool }

func (s fakeShowcaseStorage) SignedURL(_ context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	if s.fail[key] {
		return "", time.Time{}, errors.New("sign failed")
	}
	return "https://cdn.test/" + key + "?token=x", time.Now().Add(ttl), nil
}

func TestShowcaseSkipsUnsafeOrUnsignableAndCaches(t *testing.T) {
	thumb := "thumbs/u/a.jpg"
	repo := &fakeShowcaseRepo{rows: []repository_open_api.ShowcaseRow{
		{ID: uuid.New(), Title: "ok", AspectRatio: "9:16", DurationSeconds: 5, OutputMediaKey: "outputs/u/a.mp4", OutputThumbKey: &thumb},
		{ID: uuid.New(), Title: "provider", OutputMediaKey: "https://ark.example.com/video.mp4"},
		{ID: uuid.New(), Title: "broken", OutputMediaKey: "outputs/u/broken.mp4"},
	}}
	now := time.Now()
	f := NewShowcaseFacade(repo, fakeShowcaseStorage{fail: map[string]bool{"outputs/u/broken.mp4": true}}).(*showcaseFacade)
	f.now = func() time.Time { return now }

	items, err := f.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "ok" || items[0].ThumbnailURL == nil {
		t.Fatalf("unexpected items: %+v", items)
	}

	if _, err := f.List(context.Background()); err != nil || repo.calls != 1 {
		t.Fatalf("expected cached result, calls=%d err=%v", repo.calls, err)
	}
	now = now.Add(showcaseCacheTTL + time.Second)
	if _, err := f.List(context.Background()); err != nil || repo.calls != 2 {
		t.Fatalf("expected refresh after TTL, calls=%d err=%v", repo.calls, err)
	}
}
