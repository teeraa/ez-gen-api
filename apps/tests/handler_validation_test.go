package tests

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/handlers/handler_open_api"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// TestJobHandler_CreateJob_ValidationError checks that a bad request body
// returns 400 with VALIDATION_ERROR without touching the facade.
func TestJobHandler_CreateJob_ValidationError(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	// Fake auth middleware that injects a userID
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("userID", uuid.New().String())
		c.Locals("requestID", uuid.New().String())
		return c.Next()
	})

	// We wire a nil facade; the handler must reject the invalid request before calling the facade.
	jh := handler_open_api.NewJobHandler(nil)
	jh.RegisterRoutes(app)

	// Missing required fields
	body, _ := json.Marshal(map[string]any{
		"prompt": "", // too short
	})
	req := httptest.NewRequest("POST", "/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["code"] != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR code, got %q", payload["code"])
	}
}
