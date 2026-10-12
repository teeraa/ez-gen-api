package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/handlers/handler_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/gofiber/fiber/v2"
)

func TestShareHandler_StatusCodesAndCaching(t *testing.T) {
	t.Parallel()
	f, _, job := shareFixture(t, models.JobStatusSucceeded, "outputs/u/j.mp4")
	pending, _, pendingJob := shareFixture(t, models.JobStatusProcessing, "")

	newApp := func(fac facade_open_api.ShareFacade, userID string) *fiber.App {
		app := fiber.New()
		h := handler_open_api.NewShareHandler(fac)
		h.RegisterPublicRoutes(app.Group("/public/v1"))
		open := app.Group("/open/v1", func(c *fiber.Ctx) error {
			c.Locals("userID", userID)
			return c.Next()
		})
		h.RegisterRoutes(open)
		return app
	}
	app := newApp(f, job.UserID.String())

	do := func(a *fiber.App, method, path string) *http.Response {
		resp, err := a.Test(httptest.NewRequest(method, path, nil), int((5 * time.Second).Milliseconds()))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := do(app, http.MethodPost, "/open/v1/jobs/"+job.ID.String()+"/shares")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first share status = %d, want 201", resp.StatusCode)
	}
	var body struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp := do(app, http.MethodPost, "/open/v1/jobs/"+job.ID.String()+"/shares"); resp.StatusCode != http.StatusOK {
		t.Errorf("repeat share status = %d, want 200", resp.StatusCode)
	}

	pub := do(app, http.MethodGet, "/public/v1/shares/"+tokenFromURL(t, body.Data.URL))
	if pub.StatusCode != http.StatusOK || pub.Header.Get("Cache-Control") != "public, max-age=60" {
		t.Errorf("public share: status=%d cache=%q", pub.StatusCode, pub.Header.Get("Cache-Control"))
	}
	if r := do(app, http.MethodGet, "/public/v1/shares/nope"); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown token status = %d, want 404", r.StatusCode)
	}

	pendingApp := newApp(pending, pendingJob.UserID.String())
	conflict := do(pendingApp, http.MethodPost, "/open/v1/jobs/"+pendingJob.ID.String()+"/shares")
	var errBody struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(conflict.Body).Decode(&errBody)
	if conflict.StatusCode != http.StatusConflict || errBody.Code != "JOB_NOT_SHAREABLE" {
		t.Errorf("pending job share: status=%d code=%q, want 409 JOB_NOT_SHAREABLE", conflict.StatusCode, errBody.Code)
	}
}
