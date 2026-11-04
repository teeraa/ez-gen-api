package facade_open_api

import (
	"context"
	"errors"
	req "github.com/ez-gen/ez-gen-api/apps/requests/request_open_api"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestPipelineActionableValidation(t *testing.T) {
	cases := []struct {
		name, field, reason string
		mutate              func(*req.CreatePipelineRequest)
	}{
		{"reference uuid", "referenceImageId", "invalid_uuid", func(in *req.CreatePipelineRequest) { in.ReferenceImageID = "bad" }},
		{"asset uuid", "characterAssetId", "invalid_uuid", func(in *req.CreatePipelineRequest) { in.CharacterAssetID = "bad" }},
		{"key uuid", "idempotencyKey", "invalid_uuid", func(in *req.CreatePipelineRequest) { in.IdempotencyKey = "bad" }},
		{"both", "characterAssetId", "conflicting_sources", func(in *req.CreatePipelineRequest) { in.CharacterAssetID = uuid.NewString() }},
		{"blank character", "characterPrompt", "required", func(in *req.CreatePipelineRequest) { in.CharacterPrompt = " \n " }},
		{"blank scene", "scenePrompt", "required", func(in *req.CreatePipelineRequest) { in.ScenePrompt = " \n " }},
		{"long character", "characterPrompt", "too_long", func(in *req.CreatePipelineRequest) { in.CharacterPrompt = strings.Repeat("ก", 2001) }},
		{"long scene", "scenePrompt", "too_long", func(in *req.CreatePipelineRequest) { in.ScenePrompt = strings.Repeat("ก", 2001) }},
		{"long dialogue", "dialogue", "too_long", func(in *req.CreatePipelineRequest) { in.Dialogue = strings.Repeat("ก", 1001) }},
		{"quoted dialogue", "dialogue", "dialogue_encoding_budget", func(in *req.CreatePipelineRequest) { in.Dialogue = strings.Repeat("\"", 900) }},
		{"combined", "scenePrompt", "prompt_budget", func(in *req.CreatePipelineRequest) {
			in.ScenePrompt = strings.Repeat("ก", 1500)
			in.Dialogue = "สวัสดีครับทุกคน"
		}},
		{"style", "visualStyle", "invalid_option", func(in *req.CreatePipelineRequest) { in.VisualStyle = "bad" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, m, images, videos, user, in := pipelineFixture(t)
			tc.mutate(&in)
			_, err := f.Create(context.Background(), user, in)
			var detail *PipelineValidationError
			if !errors.Is(err, ErrValidation) || !errors.As(err, &detail) || detail.Field != tc.field || detail.Reason != tc.reason || detail.Message == "" {
				t.Fatalf("expected %s/%s got %#v (%v)", tc.field, tc.reason, detail, err)
			}
			if m.p != nil || images.calls != 0 || videos.calls != 0 {
				t.Fatal("invalid request created aggregate or child jobs")
			}
		})
	}
}
func TestPipelineInvalidUploadMIMEValidation(t *testing.T) {
	f, m, _, _, user, in := pipelineFixture(t)
	m.upload.MimeType = "application/pdf"
	_, err := f.Create(context.Background(), user, in)
	var detail *PipelineValidationError
	if !errors.As(err, &detail) || detail.Field != "referenceImageId" || detail.Reason != "invalid_image" {
		t.Fatalf("unexpected %v", err)
	}
}
