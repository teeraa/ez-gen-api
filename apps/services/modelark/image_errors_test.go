package modelark

import (
	"context"
	"fmt"
	sdkmodel "github.com/volcengine/ark-runtime-go/arkruntime/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImageDiagnosticsOmitProviderSecrets(t *testing.T) {
	for _, tc := range []struct{ code, param, wantCode, wantParam string }{
		{"InvalidParameter.size", "size", "InvalidParameter", "size"},
		{"InvalidParameter.SECRET_PROMPT", "https://private.test/?token=SECRET_TOKEN", "InvalidParameter", ""},
		{"SECRET_KEY\nforged=true", "SECRET_PROMPT", "", ""},
		{"InputImageSensitiveContentDetected", "image", "InputImageSensitiveContentDetected", "image"},
	} {
		err := classifyImageError(&sdkmodel.APIError{HTTPStatusCode: 400, Code: tc.code, Param: &tc.param, Message: "SECRET_KEY SECRET_PROMPT https://private.test/?token=SECRET_TOKEN"})
		d := ImageFailureDiagnostic(fmt.Errorf("wrapper: %w", err))
		if d.ProviderCode != tc.wantCode || d.Param != tc.wantParam || d.HTTPStatus != 400 {
			t.Fatalf("diagnostic %+v", d)
		}
		if strings.Contains(fmt.Sprint(d, err), "SECRET") {
			t.Fatalf("secret escaped: %+v %v", d, err)
		}
	}
}

func TestImageClientRetainsSafeHTTPAndEmbeddedDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"HTTP parameter", 400, `{"error":{"code":"InvalidParameter","param":"size","message":"SECRET_KEY"}}`, "InvalidParameter"},
		{"embedded response", 200, `{"model":"fixture","created":1,"error":{"code":"InputTextSensitiveContentDetected","message":"SECRET_KEY"}}`, "InputTextSensitiveContentDetected"},
		{"embedded item", 200, `{"model":"fixture","created":1,"data":[{"error":{"code":"OutputImageSensitiveContentDetected","message":"SECRET_KEY"}}]}`, "OutputImageSensitiveContentDetected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer s.Close()
			_, err := NewImageClient("fixture", s.URL, "fixture").GenerateImage(context.Background(), ImageRequest{Prompt: "SECRET_PROMPT", Size: "2048x2048", ReferenceURL: "https://private.test/?token=SECRET_TOKEN"})
			if err == nil {
				t.Fatal("expected failure")
			}
			d := ImageFailureDiagnostic(err)
			if d.ProviderCode != tc.want || calls != 1 || strings.Contains(fmt.Sprint(err, d), "SECRET") {
				t.Fatalf("error %v diagnostic %+v calls %d", err, d, calls)
			}
		})
	}
}

func TestImageFailureModelHintIsNotConfigurationFault(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code   string
		status int
		want   string
	}{
		{"UnknownModelRuntimeError", 0, "PROVIDER_ERROR"},
		{"InvalidParameter.ModelInput", 400, "PROVIDER_INVALID_INPUT"},
		{"ModelNotFound", 0, "MODEL_UNAVAILABLE"},
		{"", 404, "MODEL_UNAVAILABLE"},
		{"ServerOverloaded", 503, "PROVIDER_UNAVAILABLE"},
	}
	for _, tc := range cases {
		if got := imageFailure(tc.code, tc.status, nil).Code; got != tc.want {
			t.Errorf("%s/%d: got %s want %s", tc.code, tc.status, got, tc.want)
		}
	}
}
