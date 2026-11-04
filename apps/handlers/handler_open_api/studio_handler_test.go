package handler_open_api

import (
	"context"
	"encoding/json"
	"github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	"github.com/ez-gen/ez-gen-api/apps/models"
	repo "github.com/ez-gen/ez-gen-api/apps/repositories/repository_open_api"
	"github.com/gofiber/fiber/v2"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type searchRepository struct {
	repo.StudioRepository
	search string
	calls  int
}

func (r *searchRepository) Assets(_ context.Context, _, _, search string, _, _ int) ([]models.LibraryAsset, int64, error) {
	r.search = search
	r.calls++
	return []models.LibraryAsset{}, 0, nil
}
func TestAssetSearchLimitCountsThaiCharacters(t *testing.T) {
	for _, tc := range []struct {
		name        string
		count, want int
	}{{"one hundred Thai characters", 100, 200}, {"boundary two hundred Thai characters", 200, 200}, {"over two hundred Thai characters", 201, 400}} {
		t.Run(tc.name, func(t *testing.T) {
			r := &searchRepository{}
			facade := facade_open_api.NewStudioFacade(r, nil, nil, nil, "fixture", time.Hour, "", 3)
			h := NewStudioHandler(facade, 10)
			app := fiber.New()
			app.Use(func(c *fiber.Ctx) error { c.Locals("userID", "fixture-user"); return c.Next() })
			h.RegisterRoutes(app)
			search := strings.Repeat("ก", tc.count)
			response, e := app.Test(httptest.NewRequest("GET", "/assets?search="+url.QueryEscape(search), nil))
			if e != nil {
				t.Fatal(e)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.want {
				t.Fatalf("Thai runes=%d bytes=%d status=%d want=%d", tc.count, len(search), response.StatusCode, tc.want)
			}
			if tc.want == 200 {
				if r.calls != 1 || r.search != search {
					t.Fatalf("valid search not passed intact to repository: calls=%d", r.calls)
				}
			} else if r.calls != 0 {
				t.Fatal("over-limit search reached repository")
			}
		})
	}
}

func TestCharacterReferenceUnavailableHasDedicatedSafe503(t *testing.T) {
	app := fiber.New()
	app.Post("/image-jobs", func(c *fiber.Ctx) error { return studioError(c, facade_open_api.ErrCharacterReferenceUnavailable) })
	response, err := app.Test(httptest.NewRequest("POST", "/image-jobs", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 503 || body.Code != "CHARACTER_REFERENCE_UNAVAILABLE" || !strings.Contains(body.Message, "ข้อความอย่างเดียว") {
		t.Fatalf("wrong unavailable response: %d %+v", response.StatusCode, body)
	}
}
