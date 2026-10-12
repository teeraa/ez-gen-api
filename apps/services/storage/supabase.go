package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SupabaseClient talks to the Supabase Storage REST API with the service role key.
// The key grants full storage access, so it is only ever sent as a header.
type SupabaseClient struct {
	baseURL    string
	serviceKey string
	bucket     string
	http       *http.Client

	mu    sync.Mutex
	cache map[string]cachedURL
}

type cachedURL struct {
	url       string
	expiresAt time.Time
}

func NewSupabaseClient(baseURL, serviceKey, bucket string, httpClient *http.Client) (*SupabaseClient, error) {
	if baseURL == "" || serviceKey == "" || bucket == "" {
		return nil, errors.New("supabase storage: SUPABASE_URL, SUPABASE_SERVICE_ROLE_KEY and STORAGE_BUCKET are required")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Minute}
	}
	return &SupabaseClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		serviceKey: serviceKey,
		bucket:     bucket,
		http:       httpClient,
		cache:      make(map[string]cachedURL),
	}, nil
}

func (c *SupabaseClient) Upload(ctx context.Context, key string, data []byte, contentType string) error {
	endpoint := c.baseURL + "/storage/v1/object/" + url.PathEscape(c.bucket) + "/" + escapeKey(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("supabase upload %s: %w", key, err)
	}
	c.authorize(req)
	req.Header.Set("Content-Type", contentType)
	// Upsert keeps retries idempotent when a previous attempt uploaded but the DB update failed.
	req.Header.Set("x-upsert", "true")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("supabase upload %s: %w", key, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("supabase upload %s: status %d: %s", key, resp.StatusCode, readSnippet(resp.Body))
	}
	return nil
}

// SignedURL caches URLs so frequent job polling does not hit Supabase on every request.
// A cached URL is reused only while it still has at least half its lifetime left.
func (c *SupabaseClient) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	c.mu.Lock()
	if e, ok := c.cache[key]; ok && e.expiresAt.Sub(now) > ttl/2 {
		c.mu.Unlock()
		return e.url, e.expiresAt, nil
	}
	c.mu.Unlock()

	body, err := json.Marshal(map[string]int{"expiresIn": int(ttl.Seconds())})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("supabase sign %s: %w", key, err)
	}
	endpoint := c.baseURL + "/storage/v1/object/sign/" + url.PathEscape(c.bucket) + "/" + escapeKey(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("supabase sign %s: %w", key, err)
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("supabase sign %s: %w", key, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", time.Time{}, fmt.Errorf("supabase sign %s: status %d: %s", key, resp.StatusCode, readSnippet(resp.Body))
	}
	var out struct {
		SignedURL string `json:"signedURL"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", time.Time{}, fmt.Errorf("supabase sign %s: decode: %w", key, err)
	}
	if out.SignedURL == "" {
		return "", time.Time{}, fmt.Errorf("supabase sign %s: empty signedURL", key)
	}

	signed := out.SignedURL
	if !strings.HasPrefix(signed, "http") {
		if !strings.HasPrefix(signed, "/storage/v1") {
			signed = "/storage/v1" + signed
		}
		signed = c.baseURL + signed
	}
	expiresAt := now.Add(ttl)

	c.mu.Lock()
	for k, e := range c.cache {
		if now.After(e.expiresAt) {
			delete(c.cache, k)
		}
	}
	c.cache[key] = cachedURL{url: signed, expiresAt: expiresAt}
	c.mu.Unlock()
	return signed, expiresAt, nil
}

// SignedDownloadURL makes the browser save the file as filename instead of playing it.
func (c *SupabaseClient) SignedDownloadURL(ctx context.Context, key string, ttl time.Duration, filename string) (string, time.Time, error) {
	signed, expiresAt, err := c.SignedURL(ctx, key, ttl)
	if err != nil {
		return "", time.Time{}, err
	}
	return withDownloadParam(signed, filename), expiresAt, nil
}

func withDownloadParam(signed, filename string) string {
	sep := "?"
	if strings.Contains(signed, "?") {
		sep = "&"
	}
	return signed + sep + "download=" + url.QueryEscape(filename)
}

func (c *SupabaseClient) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.serviceKey)
	req.Header.Set("apikey", c.serviceKey)
}

func escapeKey(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func readSnippet(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 256))
	return strings.TrimSpace(string(b))
}
