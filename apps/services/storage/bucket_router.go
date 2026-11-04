package storage

import (
	"context"
	"strings"
	"time"
)

type bucketStore interface {
	Upload(context.Context, string, []byte, string) error
	SignedURL(context.Context, string, time.Duration) (string, time.Time, error)
	SignedDownloadURL(context.Context, string, time.Duration, string) (string, time.Time, error)
}

// BucketRouter preserves legacy object locations while new character keys use
// a separate private bucket. Persisted keys determine routing for every reader.
type BucketRouter struct{ legacy, images bucketStore }

func NewBucketRouter(legacy, images bucketStore) *BucketRouter {
	return &BucketRouter{legacy: legacy, images: images}
}
func (r *BucketRouter) store(key string) bucketStore {
	if strings.HasPrefix(key, "characters/") {
		return r.images
	}
	return r.legacy
}
func (r *BucketRouter) Upload(ctx context.Context, key string, data []byte, kind string) error {
	return r.store(key).Upload(ctx, key, data, kind)
}
func (r *BucketRouter) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	return r.store(key).SignedURL(ctx, key, ttl)
}
func (r *BucketRouter) SignedDownloadURL(ctx context.Context, key string, ttl time.Duration, filename string) (string, time.Time, error) {
	return r.store(key).SignedDownloadURL(ctx, key, ttl, filename)
}
