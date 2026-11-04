package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
