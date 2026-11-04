package handler_open_api

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type draftPipelineAPI struct {
	pipelineAPI
	created   req.CreatePipelineRequest
	finalized req.FinalizePipelineRequest
	owner, id string
}

func (m *draftPipelineAPI) Create(_ context.Context, user string, in req.CreatePipelineRequest) (*dto.PipelineDTO, error) {
	m.created = in
	m.owner = user
	return &dto.PipelineDTO{IsDraft: in.Draft}, nil
}
func (m *draftPipelineAPI) Finalize(_ context.Context, id, user string, in req.FinalizePipelineRequest) (*dto.PipelineDTO, error) {
	m.finalized = in
	m.owner = user
	m.id = id
	return &dto.PipelineDTO{}, nil
}
func TestPipelineHTTPDraftAndFinalizeBoundary(t *testing.T) {
	t.Parallel()
	mock := &draftPipelineAPI{}
	app := fiber.New()
	owner := uuid.NewString()
	id := uuid.NewString()
	key := uuid.NewString()
	app.Use(func(c *fiber.Ctx) error { c.Locals("userID", owner); return c.Next() })
	NewPipelineHandler(mock, 100).RegisterRoutes(app)
	body := `{"characterPrompt":"character","scenePrompt":"scene","draft":true,"idempotencyKey":"` + key + `"}`
	r := httptest.NewRequest("POST", "/studio-pipelines", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	res, e := app.Test(r)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 201 || !mock.created.Draft || mock.owner != owner {
		t.Fatalf("draft dropped status %d", res.StatusCode)
	}
	for _, tc := range []struct {
		body   string
		status int
	}{{`{"idempotencyKey":"` + key + `"}`, 201}, {`{"idempotencyKey":"` + key + `","sourceDraftJobId":"private"}`, 400}, {`{"idempotencyKey":"bad"}`, 400}} {
		r = httptest.NewRequest("POST", "/studio-pipelines/"+id+"/finalize", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		res, e = app.Test(r)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		if res.StatusCode != tc.status {
			t.Fatalf("body %s status %d", tc.body, res.StatusCode)
		}
	}
	if mock.finalized.IdempotencyKey != key || mock.owner != owner || mock.id != id {
		t.Fatal("finalize identity lost")
	}
}
