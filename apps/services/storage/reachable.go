package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var reachableClient = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// Reachable confirms a signed object URL serves content, so a missing object is detected before
// a paid provider request references it. A one-byte ranged GET works where HEAD is not allowed.
func Reachable(ctx context.Context, rawURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("reference url: %w", err)
	}
	req.Header.Set("Range", "bytes=0-0")
	res, err := reachableClient.Do(req)
	if err != nil {
		return fmt.Errorf("reference fetch: %w", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1024))
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("reference status %d", res.StatusCode)
	}
	return nil
}

// FetchImage downloads a signed owned-storage image for server-side analysis, bounded to max bytes.
func FetchImage(ctx context.Context, rawURL string, max int64) ([]byte, string, error) {
	return fetchOwned(ctx, rawURL, max, "image/")
}

// FetchVideo downloads a signed owned-storage video (for merging clips), bounded to max bytes.
func FetchVideo(ctx context.Context, rawURL string, max int64) ([]byte, string, error) {
	return fetchOwned(ctx, rawURL, max, "video/")
}

var ownedClient = &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func fetchOwned(ctx context.Context, rawURL string, max int64, prefix string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("media url: %w", err)
	}
	res, err := ownedClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("media fetch: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("media status %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil {
		return nil, "", fmt.Errorf("media read: %w", err)
	}
	if int64(len(data)) > max {
		return nil, "", fmt.Errorf("media exceeds %d bytes", max)
	}
	kind := http.DetectContentType(data)
	if prefix == "video/" && !strings.HasPrefix(kind, "video/") && len(data) > 8 && string(data[4:8]) == "ftyp" {
		kind = "video/mp4"
	}
	if !strings.HasPrefix(kind, prefix) {
		return nil, "", fmt.Errorf("unexpected media type %s", kind)
	}
	return data, kind, nil
}
