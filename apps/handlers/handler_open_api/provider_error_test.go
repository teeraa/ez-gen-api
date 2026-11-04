package handler_open_api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/gofiber/fiber/v2"
	sdkmodel "github.com/volcengine/ark-runtime-go/arkruntime/model"
)

func TestProviderSubmissionDiagnosticSurvivesFacadeBoundary(t *testing.T) {
	for _, tc := range []struct {
		status                 int
		providerCode, wantCode string
	}{
		{403, "AccessDenied", "PROVIDER_ACCESS_DENIED"},
		{404, "ModelNotFound", "MODEL_UNAVAILABLE"},
		{429, "RateLimit", "PROVIDER_RATE_LIMIT"},
		{400, "InvalidParameter", "PROVIDER_INVALID_INPUT"},
		{400, "InputImageSensitiveContentDetected", "PROVIDER_IMAGE_REJECTED"},
		{403, "QuotaExceeded", "PROVIDER_QUOTA"},
		{500, "InternalError", "PROVIDER_ERROR"},
	} {
		t.Run(tc.wantCode, func(t *testing.T) {
			raw := &sdkmodel.APIError{HTTPStatusCode: tc.status, Code: tc.providerCode, Message: "secret-key-and-private-prompt"}
			failure := facade_open_api.NewProviderSubmissionError(fmt.Errorf("unsafe signed-url: %w", raw))
			wrapped := fmt.Errorf("private request context: %w", failure)
			if !errors.Is(wrapped, facade_open_api.ErrProviderError) {
				t.Fatal("provider sentinel lost")
			}
			app := fiber.New()
			app.Get("/", func(c *fiber.Ctx) error { return mapFacadeError(c, wrapped) })
			response, err := app.Test(httptest.NewRequest("GET", "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			var diagnostic struct{ Code, Message string }
			if err := json.Unmarshal(body, &diagnostic); err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != 502 || diagnostic.Code != "PROVIDER_ERROR" || failure.Code() != tc.wantCode || diagnostic.Message != failure.Message() {
				t.Fatalf("response %d %s", response.StatusCode, body)
			}
			for _, private := range []string{"secret-key", "private-prompt", "signed-url", "private request"} {
				if strings.Contains(string(body), private) || strings.Contains(failure.Error(), private) {
					t.Fatalf("leaked %s", private)
				}
			}
		})
	}
}

func TestGenericProviderFailureRemainsSafe(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		return mapFacadeError(c, fmt.Errorf("secret-key: %w", facade_open_api.ErrProviderError))
	})
	response, err := app.Test(httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 502 || !strings.Contains(string(body), `"code":"PROVIDER_ERROR"`) || strings.Contains(string(body), "secret-key") {
		t.Fatalf("response %d %s", response.StatusCode, body)
	}
}
