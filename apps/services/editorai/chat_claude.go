package editorai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Claude is the Anthropic Messages API tool caller. It is selected with
// EDITOR_AI_CHAT_PROVIDER=claude and stays unavailable until a key and model are set.
type Claude struct {
	key, model, endpoint string
	client               *http.Client
}

func NewClaude(key, model string) *Claude {
	return NewClaudeWithEndpoint(key, model, "https://api.anthropic.com/v1", &http.Client{Timeout: 120 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
}

// The alternate endpoint is dependency injection for tests, never a client-supplied URL.
func NewClaudeWithEndpoint(key, model, endpoint string, client *http.Client) *Claude {
	return &Claude{key: key, model: model, endpoint: strings.TrimRight(endpoint, "/"), client: client}
}
func (c *Claude) Available() bool {
	return c != nil && c.key != "" && c.model != "" && c.client != nil
}
func (c *Claude) Name() string { return "claude" }
func (c *Claude) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   []claudeBlock   `json:"content,omitempty"`
	Source    *claudeSource   `json:"source,omitempty"`
}
type claudeSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}
type claudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func claudeImages(images []Image) []claudeBlock {
	out := []claudeBlock{}
	for _, image := range images {
		label, _ := json.Marshal(struct {
			AssetID string  `json:"assetId"`
			Time    float64 `json:"time"`
		}{image.AssetID, image.Time})
		out = append(out, claudeBlock{Type: "text", Text: string(label)}, claudeBlock{Type: "image", Source: &claudeSource{Type: "base64", MediaType: "image/jpeg", Data: base64.StdEncoding.EncodeToString(image.Data)}})
	}
	return out
}

// claudeMessages maps neutral turns to Messages API messages; a Claude assistant turn is
// replayed verbatim (its content blocks may carry thinking signatures).
func claudeMessages(turns []ChatTurn) ([]claudeMessage, error) {
	out := []claudeMessage{}
	for _, turn := range turns {
		if turn.Role == "assistant" && turn.NativeBy == "claude" && len(turn.Native) > 0 {
			out = append(out, claudeMessage{Role: "assistant", Content: turn.Native})
			continue
		}
		blocks := []claudeBlock{}
		role := "user"
		if turn.Role == "assistant" {
			role = "assistant"
			if turn.Text != "" {
				blocks = append(blocks, claudeBlock{Type: "text", Text: turn.Text})
			}
			for _, call := range turn.ToolCalls {
				blocks = append(blocks, claudeBlock{Type: "tool_use", ID: call.ID, Name: call.Name, Input: chatJSONObject(call.Args)})
			}
		} else {
			for _, result := range turn.ToolResults {
				content := append([]claudeBlock{{Type: "text", Text: string(chatJSONObject(result.Content))}}, claudeImages(result.Images)...)
				blocks = append(blocks, claudeBlock{Type: "tool_result", ToolUseID: result.CallID, Content: content})
			}
			if turn.Text != "" {
				blocks = append(blocks, claudeBlock{Type: "text", Text: turn.Text})
			}
			blocks = append(blocks, claudeImages(turn.Images)...)
		}
		if len(blocks) == 0 {
			continue
		}
		raw, e := json.Marshal(blocks)
		if e != nil {
			return nil, ErrInvalid
		}
		// Consecutive user turns are merged: the API requires alternating roles.
		if n := len(out); n > 0 && out[n-1].Role == role && role == "user" {
			var previous []json.RawMessage
			var next []json.RawMessage
			if json.Unmarshal(out[n-1].Content, &previous) == nil && json.Unmarshal(raw, &next) == nil {
				merged, _ := json.Marshal(append(previous, next...))
				out[n-1].Content = merged
				continue
			}
		}
		out = append(out, claudeMessage{Role: role, Content: raw})
	}
	return out, nil
}

func (c *Claude) Chat(ctx context.Context, in ChatRequest) (ChatReply, error) {
	if !c.Available() {
		return ChatReply{}, ErrUnavailable
	}
	messages, e := claudeMessages(in.Turns)
	if e != nil {
		return ChatReply{}, e
	}
	type tool struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	tools := make([]tool, 0, len(in.Tools))
	for _, t := range in.Tools {
		schema := t.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		tools = append(tools, tool{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	body := map[string]any{"model": c.model, "max_tokens": chatOutputTokens(in.MaxOutputTokens), "system": in.System, "messages": messages}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	raw, e := json.Marshal(body)
	if e != nil {
		return ChatReply{}, ErrInvalid
	}
	if len(raw) > chatMaxRequestBytes+(4<<20) {
		return ChatReply{}, ErrTooLarge
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/messages", bytes.NewReader(raw))
	if e != nil {
		return ChatReply{}, ErrUnavailable
	}
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	res, e := c.client.Do(req)
	if e != nil {
		return ChatReply{}, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode == 429 {
		return ChatReply{}, &RateError{RetryAt: time.Now().UTC().Add(retryAfter(res.Header.Get("Retry-After")))}
	}
	if res.StatusCode != 200 {
		log.Printf("editorai: claude chat model=%s http=%d %s", c.model, res.StatusCode, claudeErrorSummary(res.Body))
		return ChatReply{}, ErrUnavailable
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if e != nil {
		return ChatReply{}, ErrUnavailable
	}
	if len(data) > 2<<20 {
		return ChatReply{}, ErrInvalid
	}
	var out struct {
		Content    json.RawMessage `json:"content"`
		StopReason string          `json:"stop_reason"`
		Usage      struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(data, &out) != nil || out.StopReason == "max_tokens" || out.StopReason == "refusal" {
		return ChatReply{}, ErrInvalid
	}
	var blocks []claudeBlock
	if json.Unmarshal(out.Content, &blocks) != nil {
		return ChatReply{}, ErrInvalid
	}
	reply := ChatReply{Native: out.Content, NativeBy: "claude", Usage: ChatUsage{InputTokens: out.Usage.Input, OutputTokens: out.Usage.Output}}
	for _, block := range blocks {
		switch block.Type {
		case "text":
			reply.Text += block.Text
		case "tool_use":
			reply.ToolCalls = append(reply.ToolCalls, ToolCall{ID: block.ID, Name: block.Name, Args: chatJSONObject(block.Input)})
		}
	}
	if reply.Text == "" && len(reply.ToolCalls) == 0 {
		return ChatReply{}, ErrInvalid
	}
	return reply, nil
}

func claudeErrorSummary(body io.Reader) string {
	var payload struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(body, 8<<10))
	if json.Unmarshal(raw, &payload) != nil {
		return "type=- message=-"
	}
	message := strings.Join(strings.Fields(payload.Error.Message), " ")
	if r := []rune(message); len(r) > 300 {
		message = string(r[:300]) + "…"
	}
	return "type=" + payload.Error.Type + " message=" + strconv.Quote(message)
}
