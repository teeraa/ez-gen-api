package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestFixtureRefinementHTTPRoute(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "malformed"}[invalid], func(t *testing.T) {
			control := newFixtureControl()
			if invalid {
				control.modes["source"] = fixtureMode{AssetID: "source", Mode: "invalid", Remaining: 1}
			}
			app := fiber.New()
			app.Post("/fixture/gemini/models/*", control.generate)
			prompt := `{"task":"refine_transcripts","transcripts":[{"assetId":"source","language":"th","segments":[{"segmentIndex":0,"text":"คำพูดทดสอบ","uncertain":false}]}]}`
			request := map[string]any{"contents": []any{map[string]any{"parts": []any{map[string]any{"text": prompt}}}}, "generationConfig": map[string]any{"responseFormat": map[string]any{"text": map[string]any{"mimeType": "APPLICATION_JSON", "schema": map[string]any{"type": "object"}}}}}
			body, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/fixture/gemini/models/test:generateContent", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-goog-api-key", "fixture-only")
			response, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			data, err := io.ReadAll(response.Body)
			if err != nil || response.StatusCode != 200 {
				t.Fatalf("route status=%d err=%v body=%s", response.StatusCode, err, data)
			}
			var envelope struct {
				Candidates []struct {
					FinishReason string `json:"finishReason"`
					Content      struct {
						Parts []struct {
							Text string `json:"text"`
						} `json:"parts"`
					} `json:"content"`
				} `json:"candidates"`
			}
			if json.Unmarshal(data, &envelope) != nil || len(envelope.Candidates) != 1 || envelope.Candidates[0].FinishReason != "STOP" || len(envelope.Candidates[0].Content.Parts) != 1 {
				t.Fatalf("invalid provider envelope: %s", data)
			}
			result := envelope.Candidates[0].Content.Parts[0].Text
			var decoded map[string]json.RawMessage
			if json.Unmarshal([]byte(result), &decoded) != nil {
				t.Fatal("invalid result JSON")
			}
			if invalid {
				if _, ok := decoded["unexpected"]; !ok {
					t.Fatalf("invalid mode did not apply to refinement: %s", result)
				}
			} else {
				var refined struct {
					Transcripts []struct {
						AssetID  string `json:"assetId"`
						Segments []struct {
							SegmentIndex int    `json:"segmentIndex"`
							Text         string `json:"text"`
							Uncertain    bool   `json:"uncertain"`
						} `json:"segments"`
					} `json:"transcripts"`
				}
				if json.Unmarshal([]byte(result), &refined) != nil || len(refined.Transcripts) != 1 || refined.Transcripts[0].AssetID != "source" || len(refined.Transcripts[0].Segments) != 1 || refined.Transcripts[0].Segments[0].Text != "คำพูดทดสอบ" {
					t.Fatalf("refinement route response: %s", result)
				}
			}
			if control.calls["refine_transcripts"] != 1 {
				t.Fatal("refinement call not counted")
			}
		})
	}
}
