package facade_open_api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

func providerImageError(t *testing.T, status int, code string) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"code":"` + code + `","message":"fixture","type":"fixture"}}`))
	}))
	t.Cleanup(server.Close)
	_, e := modelark.NewImageClient("fixture-only", server.URL, "image-fixture").GenerateImage(context.Background(), modelark.ImageRequest{Prompt: "p", Size: "1024x1024"})
	if e == nil {
		t.Fatal("fixture must fail")
	}
	return e
}

func TestStoryDefinitiveImageErrorTreatsProviderHTTPErrorsAsFinal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  func(*testing.T) error
		want bool
	}{
		{"provider 500", func(t *testing.T) error { return providerImageError(t, 500, "InternalServiceError") }, true},
		{"provider 503", func(t *testing.T) error { return providerImageError(t, 503, "ServerOverloaded") }, true},
		{"provider 400", func(t *testing.T) error { return providerImageError(t, 400, "InvalidParameter") }, true},
		{"timeout without status", func(*testing.T) error { return &modelark.ImageError{Code: "PROVIDER_TIMEOUT"} }, false},
		{"unknown error", func(*testing.T) error { return errors.New("network reset") }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := tc.err(t)
			if _, got := storyDefinitiveImageError(e); got != tc.want {
				t.Fatalf("definitive=%v want %v (diagnostic %+v)", got, tc.want, modelark.ImageFailureDiagnostic(e))
			}
		})
	}
}
