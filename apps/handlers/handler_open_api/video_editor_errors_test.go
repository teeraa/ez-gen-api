package handler_open_api

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/gofiber/fiber/v2"
)

func TestVideoEditorUnavailableErrorsMapTo503(t *testing.T) {
	t.Parallel()
	for name, e := range map[string]error{"schema": f.ErrVESchema, "text": f.ErrVETextUnavailable, "media": f.ErrVEMedia, "wrapped schema": errors.Join(errors.New("ctx"), f.ErrVESchema)} {
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Get("/", func(c *fiber.Ctx) error { return veHTTPError(c, e) })
		res, err := app.Test(httptest.NewRequest("GET", "/", nil))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != 503 || !strings.Contains(string(body), "MEDIA_UNAVAILABLE") {
			t.Errorf("%s: %d %s", name, res.StatusCode, body)
		}
	}
}
