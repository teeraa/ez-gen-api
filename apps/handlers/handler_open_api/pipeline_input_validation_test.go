package handler_open_api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	dto "github.com/ez-gen/ez-gen-api/apps/dto/dto_open_api"
	f "github.com/ez-gen/ez-gen-api/apps/facades/facade_open_api"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type pipelineInputAPI struct {
	pipelineAPI
	calls int
	last  req.CreatePipelineRequest
}

func (m *pipelineInputAPI) Create(_ context.Context, _ string, in req.CreatePipelineRequest) (*dto.PipelineDTO, error) {
	m.calls++
	m.last = in
	return &dto.PipelineDTO{}, nil
}
func inputApp(mock *pipelineInputAPI) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("userID", "fixture-owner")
		c.Locals("requestID", "fixture-request-id")
		return c.Next()
	})
	NewPipelineHandler(mock, 100000).RegisterRoutes(app)
	return app
}
func inputHTTP(t *testing.T, app *fiber.App, body []byte) (int, []byte) {
	t.Helper()
	request := httptest.NewRequest("POST", "/studio-pipelines", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, raw
}
func normalPipelineInput() map[string]any {
	return map[string]any{"characterPrompt": "character", "scenePrompt": "scene", "visualStyle": "cinematic_realistic", "aspectRatio": "9:16", "durationSeconds": 5, "modelId": "violet-realism", "quality": "standard", "draft": true, "idempotencyKey": uuid.NewString()}
}
func TestPipelineCreateStructuredInputErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		field  string
		value  any
		reason string
	}{
		{"referenceImageId", "not-a-uuid", "invalid_uuid"}, {"characterAssetId", "not-a-uuid", "invalid_uuid"}, {"idempotencyKey", "not-a-uuid", "invalid_uuid"}, {"idempotencyKey", "", "required"},
		{"scenePrompt", "", "required"}, {"scenePrompt", strings.Repeat("ก", 2001), "too_long"}, {"characterPrompt", strings.Repeat("ก", 2001), "too_long"}, {"dialogue", strings.Repeat("ก", 1001), "too_long"},
		{"visualStyle", "unknown-private-style", "invalid_option"}, {"aspectRatio", "21:9", "invalid_option"}, {"modelId", "private-model", "invalid_option"}, {"quality", "private-quality", "invalid_option"},
		{"durationSeconds", 3, "out_of_range"}, {"durationSeconds", 16, "out_of_range"},
		{"draft", "private-bool", "invalid_type"}, {"durationSeconds", "private-duration", "invalid_type"}, {"scenePrompt", map[string]string{"private": "value"}, "invalid_type"}, {"referenceImageId", 12, "invalid_type"},
	} {
		t.Run(tc.field+"/"+tc.reason, func(t *testing.T) {
			mock := &pipelineInputAPI{}
			app := inputApp(mock)
			body := normalPipelineInput()
			body[tc.field] = tc.value
			encoded, _ := json.Marshal(body)
			status, raw := inputHTTP(t, app, encoded)
			var out struct {
				Code, Message, RequestID string
				Details                  []pipelineValidationDetail
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatal(err)
			}
			if status != 400 || out.Code != "VALIDATION_ERROR" || out.RequestID != "fixture-request-id" || len(out.Details) != 1 || out.Details[0].Field != tc.field || out.Details[0].Reason != tc.reason || out.Details[0].Message == "" || out.Message != out.Details[0].Message || mock.calls != 0 {
				t.Fatalf("unsafe or missing field response: %d %s calls%d", status, raw, mock.calls)
			}
			if bytes.Contains(raw, []byte("private-")) || bytes.Contains(raw, []byte("cannot unmarshal")) || bytes.Contains(raw, []byte(strings.Repeat("ก", 100))) {
				t.Fatalf("input leaked: %s", raw)
			}
		})
	}
}
func TestPipelineCreateMalformedInputAndUUIDVersion(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"", `null`, `[]`, `{"draft":`, `{"scenePrompt":"private-input"} trailing`} {
		mock := &pipelineInputAPI{}
		status, raw := inputHTTP(t, inputApp(mock), []byte(body))
		var out struct{ Details []pipelineValidationDetail }
		_ = json.Unmarshal(raw, &out)
		if status != 400 || len(out.Details) != 1 || out.Details[0].Field != "body" || out.Details[0].Reason != "malformed_json" || mock.calls != 0 {
			t.Fatalf("malformed %q -> %d %s", body, status, raw)
		}
	}
	// Pipeline saveAsset creates UUIDv5; ordinary UUIDv4/v7 assets remain valid identifiers.
	v7, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	versions := []uuid.UUID{uuid.New(), uuid.NewSHA1(uuid.New(), []byte("character")), v7}
	for _, id := range versions {
		mock := &pipelineInputAPI{}
		body := normalPipelineInput()
		body["characterAssetId"] = id.String()
		body["characterPrompt"] = ""
		encoded, _ := json.Marshal(body)
		status, raw := inputHTTP(t, inputApp(mock), encoded)
		if status != 201 || mock.calls != 1 || mock.last.CharacterAssetID != id.String() {
			t.Fatalf("valid character UUIDv%d rejected %d %s", id.Version(), status, raw)
		}
	}
	for _, id := range versions[1:] {
		for _, field := range []string{"referenceImageId", "idempotencyKey"} {
			mock := &pipelineInputAPI{}
			body := normalPipelineInput()
			body[field] = id.String()
			encoded, _ := json.Marshal(body)
			status, raw := inputHTTP(t, inputApp(mock), encoded)
			if status != 400 || !bytes.Contains(raw, []byte(`"reason":"invalid_uuid"`)) || mock.calls != 0 {
				t.Fatalf("strict v4 field %s accepted UUIDv%d %d %s", field, id.Version(), status, raw)
			}
		}
	}
	mock := &pipelineInputAPI{}
	body := normalPipelineInput()
	for _, field := range []string{"referenceImageId", "characterAssetId", "idempotencyKey"} {
		body[field] = "invalid"
	}
	body["scenePrompt"] = ""
	body["characterPrompt"] = strings.Repeat("x", 2001)
	body["dialogue"] = strings.Repeat("x", 1001)
	body["visualStyle"] = "invalid"
	body["aspectRatio"] = "invalid"
	body["modelId"] = "invalid"
	body["quality"] = "invalid"
	body["durationSeconds"] = 99
	encoded, _ := json.Marshal(body)
	_, raw := inputHTTP(t, inputApp(mock), encoded)
	var out struct{ Details []pipelineValidationDetail }
	_ = json.Unmarshal(raw, &out)
	if len(out.Details) != 8 {
		t.Fatalf("detail bound missing %s", raw)
	}
}

type validationImageCapabilities struct{}

func (validationImageCapabilities) Capabilities() dto.ImageCapabilitiesDTO {
	return dto.ImageCapabilitiesDTO{Enabled: true, SupportsCharacterReferenceImage: true, AspectRatios: []string{"16:9", "9:16", "1:1", "4:3", "3:4"}}
}
func (validationImageCapabilities) PipelineImageModel() string { return "fixture-image" }
func (validationImageCapabilities) CreatePipelineImage(context.Context, string, req.CreateImageRequest, string, string) (*dto.CreateJobResponseDTO, error) {
	panic("capabilities test must not generate")
}
func (validationImageCapabilities) CreateImage(context.Context, string, req.CreateImageRequest) (*dto.CreateJobResponseDTO, error) {
	panic("capabilities test must not generate")
}
func TestPipelineParserAcceptsEveryAdvertisedCombination(t *testing.T) {
	t.Parallel()
	registry, err := modelark.NewRegistry("")
	if err != nil {
		t.Fatal(err)
	}
	real := f.NewPipelineFacade(nil, validationImageCapabilities{}, nil, nil, nil, nil, registry, nil, true)
	caps := real.Capabilities()
	mock := &pipelineInputAPI{}
	app := inputApp(mock)
	count := 0
	for _, model := range caps.Models {
		for _, source := range []string{"text", "upload", "library"} {
			ratios, durations := model.AspectRatios, model.Durations
			enabled := model.TextEnabled
			if source == "upload" {
				enabled = model.Enabled
			}
			if source == "library" {
				ratios, durations, enabled = model.LibraryAspectRatios, model.LibraryDurations, model.LibraryEnabled
			}
			if !enabled {
				continue
			}
			for _, ratio := range ratios {
				for _, duration := range durations {
					for _, draft := range []bool{false, true} {
						if draft && !model.SupportsDraft {
							continue
						}
						for _, quality := range []string{"standard", "high"} {
							input := normalPipelineInput()
							input["modelId"] = model.ID
							input["aspectRatio"] = ratio
							input["durationSeconds"] = duration
							input["draft"] = draft
							input["quality"] = quality
							if source == "upload" {
								input["referenceImageId"] = uuid.NewString()
							}
							if source == "library" {
								input["characterAssetId"] = uuid.NewString()
								input["characterPrompt"] = ""
							}
							encoded, _ := json.Marshal(input)
							status, raw := inputHTTP(t, app, encoded)
							if status != 201 {
								t.Fatalf("advertised %s %s %s %d draft%t quality%s: %d %s", model.ID, source, ratio, duration, draft, quality, status, raw)
							}
							if mock.last.Draft != draft {
								t.Fatal("draft lost")
							}
							count++
						}
					}
				}
			}
		}
	}
	if count == 0 || mock.calls != count {
		t.Fatalf("no combinations tested count%d calls%d", count, mock.calls)
	}
	t.Logf("accepted %d advertised source/model/ratio/duration/draft/quality combinations", count)
	// Legacy requests may omit optional settings and draft entirely.
	status, raw := inputHTTP(t, app, []byte(fmt.Sprintf(`{"characterPrompt":"character","scenePrompt":"scene","idempotencyKey":%q}`, uuid.NewString())))
	if status != 201 || mock.last.Draft {
		t.Fatalf("legacy %d %s", status, raw)
	}
}

func TestPipelineFinalizeKeyRemainsUUIDv4(t *testing.T) {
	t.Parallel()
	v7, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []uuid.UUID{uuid.NewSHA1(uuid.New(), []byte("final-key")), v7} {
		mock := &draftPipelineAPI{}
		app := fiber.New()
		app.Use(func(c *fiber.Ctx) error { c.Locals("userID", "fixture-owner"); return c.Next() })
		NewPipelineHandler(mock, 100).RegisterRoutes(app)
		body := fmt.Sprintf(`{"idempotencyKey":%q}`, id.String())
		request := httptest.NewRequest("POST", "/studio-pipelines/"+uuid.NewString()+"/finalize", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response, e := app.Test(request)
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		if response.StatusCode != 400 || mock.finalized.IdempotencyKey != "" {
			t.Fatalf("non-v4 final key accepted %d", response.StatusCode)
		}
	}
}
