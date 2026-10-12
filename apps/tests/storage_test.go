package tests

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/services/storage"
)

func TestSupabaseClient_UploadSendsServiceKeyAndUpsert(t *testing.T) {
	t.Parallel()
	var gotPath, gotAuth, gotUpsert, gotType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		gotUpsert, gotType = r.Header.Get("x-upsert"), r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := storage.NewSupabaseClient(srv.URL, "service-key", "violet-media", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Upload(t.Context(), "outputs/u1/j1.mp4", []byte("data"), "video/mp4"); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if gotPath != "/storage/v1/object/violet-media/outputs/u1/j1.mp4" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer service-key" || gotUpsert != "true" || gotType != "video/mp4" || gotBody != "data" {
		t.Errorf("unexpected request: auth=%q upsert=%q type=%q body=%q", gotAuth, gotUpsert, gotType, gotBody)
	}
}

func TestSupabaseClient_UploadErrorDoesNotLeakKey(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"Bucket not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	c, _ := storage.NewSupabaseClient(srv.URL, "service-key", "violet-media", srv.Client())
	err := c.Upload(t.Context(), "outputs/u1/j1.mp4", []byte("x"), "video/mp4")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
	if strings.Contains(err.Error(), "service-key") {
		t.Error("error message leaks the service role key")
	}
}

func TestSupabaseClient_SignedURLBuildsAbsoluteURLAndCaches(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	var gotExpires int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			ExpiresIn int `json:"expiresIn"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotExpires = body.ExpiresIn
		_ = json.NewEncoder(w).Encode(map[string]string{
			"signedURL": "/object/sign/violet-media/outputs/u1/j1.mp4?token=tkn",
		})
	}))
	defer srv.Close()

	c, _ := storage.NewSupabaseClient(srv.URL, "service-key", "violet-media", srv.Client())
	for range 3 {
		u, exp, err := c.SignedURL(t.Context(), "outputs/u1/j1.mp4", time.Hour)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		if u != srv.URL+"/storage/v1/object/sign/violet-media/outputs/u1/j1.mp4?token=tkn" {
			t.Errorf("signed url = %q", u)
		}
		if time.Until(exp) < 50*time.Minute {
			t.Errorf("expiry too soon: %v", exp)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("expected cached signed URL, got %d sign calls", calls.Load())
	}
	if gotExpires != 3600 {
		t.Errorf("expiresIn = %d, want 3600", gotExpires)
	}
}

func TestNewSupabaseClient_RequiresConfig(t *testing.T) {
	t.Parallel()
	if _, err := storage.NewSupabaseClient("", "k", "b", nil); err == nil {
		t.Error("expected error for missing SUPABASE_URL")
	}
}

func TestHTTPFetcher(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer srv.Close()

	allowed := storage.NewHTTPFetcher(srv.Client(), []string{"127.0.0.1"}, 1024)
	data, ct, err := allowed.Fetch(t.Context(), srv.URL+"/out.mp4")
	if err != nil || string(data) != "0123456789" || ct != "video/mp4" {
		t.Fatalf("fetch = %q, %q, %v", data, ct, err)
	}

	cases := []struct {
		name    string
		fetcher *storage.HTTPFetcher
		url     string
	}{
		{"host not allowlisted", storage.NewHTTPFetcher(srv.Client(), storage.ProviderHostSuffixes, 1024), srv.URL + "/out.mp4"},
		{"plain http rejected", allowed, "http://127.0.0.1/out.mp4"},
		{"lookalike host rejected", storage.NewHTTPFetcher(srv.Client(), storage.ProviderHostSuffixes, 1024), "https://evilvolces.com/x.mp4"},
		{"oversized body rejected", storage.NewHTTPFetcher(srv.Client(), []string{"127.0.0.1"}, 5), srv.URL + "/out.mp4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := tc.fetcher.Fetch(t.Context(), tc.url); err == nil {
				t.Error("expected error")
			}
		})
	}
	if _, _, err := storage.NewHTTPFetcher(nil, storage.ProviderHostSuffixes, 1).Fetch(t.Context(), "https://evil.example/x"); !errors.Is(err, storage.ErrHostNotAllowed) {
		t.Errorf("expected ErrHostNotAllowed, got %v", err)
	}
}

func TestSupabaseClient_SignedDownloadURLAddsDownloadParam(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"signedURL": "/object/sign/b/outputs/u/j.mp4?token=t"})
	}))
	defer srv.Close()
	c, _ := storage.NewSupabaseClient(srv.URL, "k", "b", srv.Client())

	u, _, err := c.SignedDownloadURL(t.Context(), "outputs/u/j.mp4", time.Hour, "violet studio.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(u, "?token=t&download=violet+studio.mp4") {
		t.Errorf("download url = %q", u)
	}
}
