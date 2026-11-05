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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("image url: %w", err)
	}
	res, err := reachableClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("image fetch: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("image status %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil {
		return nil, "", fmt.Errorf("image read: %w", err)
	}
	if int64(len(data)) > max {
		return nil, "", fmt.Errorf("image exceeds %d bytes", max)
	}
	kind := http.DetectContentType(data)
	if !strings.HasPrefix(kind, "image/") {
		return nil, "", fmt.Errorf("not an image: %s", kind)
	}
	return data, kind, nil
}
