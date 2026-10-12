package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ProviderHostSuffixes lists the hosts BytePlus serves generated media from.
// Fetching only from these keeps a tampered provider response from turning the
// server into an open proxy (SSRF).
var ProviderHostSuffixes = []string{"volces.com", "bytepluses.com", "byteplus.com"}

var ErrHostNotAllowed = errors.New("media host not allowed")

// HTTPFetcher downloads provider output into memory. Outputs are short clips,
// so buffering keeps uploads simple and gives Supabase a known Content-Length.
type HTTPFetcher struct {
	http         *http.Client
	hostSuffixes []string
	maxBytes     int64
}

func NewHTTPFetcher(httpClient *http.Client, hostSuffixes []string, maxBytes int64) *HTTPFetcher {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	return &HTTPFetcher{http: httpClient, hostSuffixes: hostSuffixes, maxBytes: maxBytes}
}

func (f *HTTPFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", errors.New("fetch media: invalid url")
	}
	if u.Scheme != "https" || !f.hostAllowed(u.Hostname()) {
		return nil, "", fmt.Errorf("fetch media from %q: %w", u.Hostname(), ErrHostNotAllowed)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("fetch media: %w", err)
	}
	resp, err := f.http.Do(req)
	if err != nil {
		// url.Error embeds the full signed URL; keep only the cause out of the logs.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, "", fmt.Errorf("fetch media from %s: %w", u.Hostname(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("fetch media from %s: status %d", u.Hostname(), resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("fetch media from %s: read: %w", u.Hostname(), err)
	}
	if int64(len(data)) > f.maxBytes {
		return nil, "", fmt.Errorf("fetch media from %s: exceeds %d bytes", u.Hostname(), f.maxBytes)
	}

	contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	return data, contentType, nil
}

func (f *HTTPFetcher) hostAllowed(host string) bool {
	host = strings.ToLower(host)
	for _, s := range f.hostSuffixes {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}
