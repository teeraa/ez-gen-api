//go:build geminiprobe

package facade_open_api

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/editorai"
)

// Live diagnosis only (paid, a few tiny calls). Never part of the normal suite:
//
//	set -a; . ./.env; set +a
//	go test -tags geminiprobe -run TestGeminiLiveSchemaProbe -v ./apps/facades/facade_open_api
func TestGeminiLiveSchemaProbe(t *testing.T) {
	key, model := os.Getenv("GEMINI_API_KEY"), os.Getenv("GEMINI_MODEL")
	if key == "" || model == "" {
		t.Skip("GEMINI_API_KEY / GEMINI_MODEL not set")
	}
	g := editorai.NewGemini(key, model, "probe")
	tiny := json.RawMessage(`{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"}}}`)
	evidence := []models.EditorAIEvidence{{ID: "e1", AssetID: "a", Start: 0, End: 4}, {ID: "e2", AssetID: "a", Start: 4, End: 9}}
	variants := []struct {
		name   string
		tokens int
		schema json.RawMessage
	}{
		{"tiny schema, 8192 tokens", editorai.DefaultMaxOutputTokens, tiny},
		{"tiny schema, 32768 tokens", aiStructuredOutputTokens, tiny},
		{"edit schema, 8192 tokens", editorai.DefaultMaxOutputTokens, aiEditSchema},
		{"edit schema without min/max, 8192 tokens", editorai.DefaultMaxOutputTokens, aiProbeStrip(aiEditSchema, "minimum", "maximum")},
		{"edit schema without min/max/description, 8192 tokens", editorai.DefaultMaxOutputTokens, aiProbeStrip(aiEditSchema, "minimum", "maximum", "description")},
		{"plan schema, 8192 tokens", editorai.DefaultMaxOutputTokens, aiPlanSchema(evidence, 8)},
		{"plan schema without min/max, 8192 tokens", editorai.DefaultMaxOutputTokens, aiProbeStrip(aiPlanSchema(evidence, 8), "minimum", "maximum")},
	}
	for _, v := range variants {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		_, err := g.Generate(editorai.WithMaxOutputTokens(ctx, v.tokens), "You are a test endpoint. Reply with the smallest valid JSON for the schema.", "Return a minimal valid JSON object.", nil, v.schema)
		cancel()
		result := "OK"
		if err != nil {
			result = "FAIL: " + err.Error()
		}
		t.Logf("%-52s %s", v.name, result)
	}
}

func aiProbeStrip(schema json.RawMessage, keys ...string) json.RawMessage {
	var v any
	_ = json.Unmarshal(schema, &v)
	var walk func(any)
	walk = func(n any) {
		switch x := n.(type) {
		case map[string]any:
			for _, k := range keys {
				if _, isSchemaKey := x["type"]; isSchemaKey {
					delete(x, k)
				}
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(v)
	out, _ := json.Marshal(v)
	return out
}
