package handler_open_api

import (
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	facade "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/gofiber/fiber/v2"
)

func TestWrappedVideoPromptBudgetMapsToSafe422(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		return mapFacadeError(c, fmt.Errorf("private-prompt-content: %w", facade.ErrVideoPromptBudget))
	})
	response, e := app.Test(httptest.NewRequest("GET", "/", nil))
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	body, e := io.ReadAll(response.Body)
	if e != nil {
		t.Fatal(e)
	}
	if response.StatusCode != 422 || !strings.Contains(string(body), `"errorCode":"PROMPT_TOO_LONG"`) || strings.Contains(string(body), "private-prompt-content") {
		t.Fatalf("bad budget response %d %s", response.StatusCode, body)
	}
}
