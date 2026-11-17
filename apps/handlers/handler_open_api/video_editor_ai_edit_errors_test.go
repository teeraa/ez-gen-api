package handler_open_api

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
	"github.com/gofiber/fiber/v2"
)

func TestAIEditPreviewErrorMapping(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"input":      {f.ErrAIEditInput, 400, "VALIDATION_ERROR"},
		"no changes": {f.ErrAIEditNoChanges, 422, "AI_EDIT_NO_CHANGES"},
		"base":       {f.ErrAIEditBaseInvalid, 422, "AI_EDIT_BASE_INVALID"},
		"busy":       {f.ErrAIEditBusy, 429, "AI_RATE_LIMITED"},
		"daily":      {repo.ErrAIDaily, 429, "AI_DAILY_LIMIT"},
		"quota":      {&editorai.RateError{RetryAt: time.Now()}, 429, "AI_RATE_LIMITED"},
		"provider":   {f.ErrAIEditProvider, 503, "AI_PROVIDER_UNAVAILABLE"},
		"disabled":   {editorai.ErrUnavailable, 503, "AI_UNAVAILABLE"},
		"revision":   {repo.ErrVERevision, 409, "REVISION_CONFLICT"},
		"missing":    {repo.ErrNotFound, 404, "NOT_FOUND"},
		"model":      {editorai.ErrInvalid, 422, "AI_INVALID_RESULT"},
		"editor 422": {f.ErrVEInvalid, 422, "VALIDATION_ERROR"},
	} {
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/", func(c *fiber.Ctx) error { return aiHTTPError(c, tc.err) })
		res, err := app.Test(httptest.NewRequest("GET", "/", nil))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != tc.status || !strings.Contains(string(body), `"`+tc.code+`"`) {
			t.Errorf("%s: %d %s", name, res.StatusCode, body)
		}
	}
	if !errors.Is(f.ErrAIEditInput, f.ErrVEInvalid) {
		t.Fatal("input error should still be a validation error")
	}
}

func TestAIEditPreviewRouteRejectsMalformedBody(t *testing.T) {
	t.Parallel()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Use(func(c *fiber.Ctx) error { c.Locals("userID", "8f9e2c1a-1111-4222-8333-444455556666"); return c.Next() })
	NewVideoEditorAIHandler(f.NewVideoEditorAIFacade(nil, f.NewVideoEditorFacade(nil, nil, nil), nil, nil, f.EditorAIOptions{})).RegisterRoutes(app)
	request := httptest.NewRequest("POST", "/video-editor/projects/8f9e2c1a-1111-4222-8333-444455556667/ai-edits/preview", strings.NewReader(`{"instruction":`))
	request.Header.Set("Content-Type", "application/json")
	res, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 400 || !strings.Contains(string(body), "VALIDATION_ERROR") {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	request = httptest.NewRequest("POST", "/video-editor/projects/8f9e2c1a-1111-4222-8333-444455556667/ai-edits/preview", strings.NewReader(`{"instruction":"ตัดต่อ","revision":1}`))
	request.Header.Set("Content-Type", "application/json")
	if res, err = app.Test(request); err != nil || res.StatusCode != 503 {
		t.Fatalf("disabled AI must be 503: %v %v", res.StatusCode, err)
	}
}
