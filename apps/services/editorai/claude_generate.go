package editorai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"
)

// Claude also serves as the AI editor's structured-output Provider (EDITOR_AI_PROVIDER=claude).
// The JSON schema becomes the input schema of one forced tool, so the reply is that tool's
// input: JSON in the requested shape, like Gemini's responseFormat.

func (c *Claude) Signature() string {
	return "claude:" + c.model + ":editor-prompts-v3:frames512x3-v1"
}

const claudeRespondTool = "respond"

// claudeToolSchema returns an object schema for the tool and whether the answer is wrapped
// (a tool input must be an object; other schemas are put under "result").
func claudeToolSchema(schema json.RawMessage) (json.RawMessage, bool) {
	var top struct {
		Type string `json:"type"`
	}
	if len(schema) == 0 {
		return json.RawMessage(`{"type":"object"}`), false
	}
	if json.Unmarshal(schema, &top) == nil && top.Type == "object" {
		return schema, false
	}
	wrapped, _ := json.Marshal(map[string]any{"type": "object", "properties": map[string]json.RawMessage{"result": schema}, "required": []string{"result"}})
	return wrapped, true
}

func (c *Claude) Generate(ctx context.Context, system, prompt string, images []Image, schema json.RawMessage) ([]byte, error) {
	if !c.Available() {
		return nil, ErrUnavailable
	}
	if len(prompt) > 96<<10 || len(images) > 24 {
		return nil, ErrTooLarge
	}
	n := 0
	for _, image := range images {
		n += len(image.Data)
		if n > 3<<20 {
			return nil, ErrTooLarge
		}
	}
	content, e := json.Marshal(append([]claudeBlock{{Type: "text", Text: prompt}}, claudeImages(images)...))
	if e != nil {
		return nil, ErrInvalid
	}
	input, wrapped := claudeToolSchema(schema)
	body := map[string]any{
		"model":       c.model,
		"max_tokens":  MaxOutputTokens(ctx),
		"system":      system,
		"messages":    []claudeMessage{{Role: "user", Content: content}},
		"tools":       []map[string]any{{"name": claudeRespondTool, "description": "Return the answer in exactly this structure.", "input_schema": input}},
		"tool_choice": map[string]any{"type": "tool", "name": claudeRespondTool},
	}
	raw, e := json.Marshal(body)
	if e != nil {
		return nil, ErrInvalid
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/messages", bytes.NewReader(raw))
	if e != nil {
		return nil, ErrUnavailable
	}
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	res, e := c.client.Do(req)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == 429 || res.StatusCode == 529 {
		return nil, &RateError{RetryAt: time.Now().UTC().Add(retryAfter(res.Header.Get("Retry-After")))}
	}
	if res.StatusCode != 200 {
		log.Printf("editorai: claude generate model=%s http=%d %s", c.model, res.StatusCode, claudeErrorSummary(res.Body))
		return nil, ErrUnavailable
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if e != nil {
		return nil, ErrUnavailable
	}
	if len(data) > 2<<20 {
		return nil, ErrInvalid
	}
	var out struct {
		Content    []claudeBlock `json:"content"`
		StopReason string        `json:"stop_reason"`
	}
	if json.Unmarshal(data, &out) != nil || out.StopReason != "tool_use" {
		return nil, ErrInvalid
	}
	for _, b := range out.Content {
		if b.Type != "tool_use" || b.Name != claudeRespondTool || !json.Valid(b.Input) {
			continue
		}
		if !wrapped {
			return b.Input, nil
		}
		var w struct {
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(b.Input, &w) != nil || len(w.Result) == 0 {
			return nil, ErrInvalid
		}
		return w.Result, nil
	}
	return nil, ErrInvalid
}
