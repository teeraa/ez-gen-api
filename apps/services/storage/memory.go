package storage

import (
	"context"
	"sync"
	"time"
)

// MemoryClient is an in-memory test double for SupabaseClient.
type MemoryClient struct {
	mu      sync.RWMutex
	objects map[string]MemoryObject
	// UploadErr, if non-nil, is returned by Upload.
	UploadErr error
}

type MemoryObject struct {
	Data        []byte
	ContentType string
}

func NewMemoryClient() *MemoryClient {
	return &MemoryClient{objects: make(map[string]MemoryObject)}
}

func (m *MemoryClient) Upload(_ context.Context, key string, data []byte, contentType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.UploadErr != nil {
		return m.UploadErr
	}
	m.objects[key] = MemoryObject{Data: append([]byte(nil), data...), ContentType: contentType}
	return nil
}

func (m *MemoryClient) SignedURL(_ context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	return "https://storage.test/signed/" + key, time.Now().Add(ttl), nil
}

func (m *MemoryClient) SignedDownloadURL(ctx context.Context, key string, ttl time.Duration, filename string) (string, time.Time, error) {
	signed, exp, err := m.SignedURL(ctx, key, ttl)
	return withDownloadParam(signed, filename), exp, err
}

func (m *MemoryClient) Object(key string) (MemoryObject, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	o, ok := m.objects[key]
	return o, ok
}
