package handler_open_api

import (
	"encoding/json"
	"fmt"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/gofiber/fiber/v2"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPipelineValidationErrorResponse(t *testing.T) {
	const message = "รายละเอียดฉากและบทพูดรวมกันยาวเกินขีดจำกัด กรุณาย่อฉากหรือบทพูดแล้วลองอีกครั้ง"
	app := fiber.New()
	app.Post("/pipeline", func(c *fiber.Ctx) error {
		c.Locals("requestID", "fixture-rid")
		return pipelineError(c, fmt.Errorf("private provider detail: %w", &f.PipelineValidationError{Field: "scenePrompt", Reason: "prompt_budget", Message: message}))
	})
	response, err := app.Test(httptest.NewRequest("POST", "/pipeline", strings.NewReader(`{"scenePrompt":"private user prompt"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Code, Message, RequestID string
		Details                  []pipelineValidationDetail
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 400 || out.Code != "VALIDATION_ERROR" || out.Message != message || out.RequestID != "fixture-rid" || len(out.Details) != 1 || out.Details[0].Field != "scenePrompt" || out.Details[0].Reason != "prompt_budget" || out.Details[0].Message != message {
		t.Fatalf("response %d %s", response.StatusCode, raw)
	}
	for _, secret := range []string{"private provider detail", "private user prompt", "https://"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private data leaked")
		}
	}
}
