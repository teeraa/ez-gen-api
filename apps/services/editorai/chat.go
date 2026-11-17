package editorai

import (
	"context"
	"encoding/json"
	"strings"
)

// Model-neutral tool calling for the AI chat editor. The agent loop speaks only these
// types, so the provider (Gemini today, Claude later) is a configuration choice.

// ChatTool declares one function the model may call. Parameters is a small JSON Schema
// (type/properties/required/items/enum/description only) that every provider accepts.
type ChatTool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolCall is one function call the model asked for; Args is a JSON object.
type ToolCall struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

// ToolResult answers one ToolCall. Content is a JSON object; Images ride along for
// tools that let the model look at frames.
type ToolResult struct {
	CallID  string          `json:"callId"`
	Name    string          `json:"name"`
	Content json.RawMessage `json:"content"`
	Images  []Image         `json:"-"`
}

// ChatTurn is one message in the conversation. A user turn carries Text, Images or
// ToolResults; an assistant turn carries Text and ToolCalls. Native is the provider's
// own encoding of an assistant turn (it may hold reasoning signatures that must be sent
// back verbatim); it is used only when NativeBy matches the provider replaying it.
type ChatTurn struct {
	Role        string          `json:"role"`
	Text        string          `json:"text,omitempty"`
	Images      []Image         `json:"-"`
	ToolCalls   []ToolCall      `json:"toolCalls,omitempty"`
	ToolResults []ToolResult    `json:"toolResults,omitempty"`
	Native      json.RawMessage `json:"native,omitempty"`
	NativeBy    string          `json:"nativeBy,omitempty"`
}

type ChatRequest struct {
	System          string
	Turns           []ChatTurn
	Tools           []ChatTool
	MaxOutputTokens int
}

type ChatUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

type ChatReply struct {
	Text      string
	ToolCalls []ToolCall
	Native    json.RawMessage
	NativeBy  string
	Usage     ChatUsage
}

// Turn is the reply as an assistant turn for the next request.
func (r ChatReply) Turn() ChatTurn {
	return ChatTurn{Role: "assistant", Text: r.Text, ToolCalls: r.ToolCalls, Native: r.Native, NativeBy: r.NativeBy}
}

// ToolCaller is a provider that supports multi-turn function calling.
type ToolCaller interface {
	Available() bool
	// Name identifies the provider family ("gemini", "claude"); it keys Native replay.
	Name() string
	Model() string
	Chat(context.Context, ChatRequest) (ChatReply, error)
}

// SelectToolCaller picks the chat provider by name; anything unknown falls back to Gemini.
func SelectToolCaller(name string, gemini *Gemini, claude *Claude) ToolCaller {
	if strings.EqualFold(strings.TrimSpace(name), "claude") {
		return claude
	}
	return gemini
}

// ChatModelID names a caller as "provider:model", the id clients send to pick a model.
func ChatModelID(c ToolCaller) string { return c.Name() + ":" + c.Model() }

// ParseChatModels builds one caller per "provider:model" entry of a comma-separated
// allowlist (EDITOR_AI_CHAT_MODELS). Unknown providers, empty models and duplicates are
// skipped; a model whose provider has no key is kept but reports Available() == false.
func ParseChatModels(spec, geminiKey, geminiVersion, anthropicKey string) []ToolCaller {
	out, seen := []ToolCaller{}, map[string]bool{}
	for _, entry := range strings.Split(spec, ",") {
		provider, model, ok := strings.Cut(strings.TrimSpace(entry), ":")
		provider, model = strings.ToLower(strings.TrimSpace(provider)), strings.TrimSpace(model)
		if !ok || model == "" || seen[provider+":"+model] {
			continue
		}
		switch provider {
		case "gemini":
			out = append(out, NewGemini(geminiKey, model, geminiVersion))
		case "claude":
			out = append(out, NewClaude(anthropicKey, model))
		default:
			continue
		}
		seen[provider+":"+model] = true
	}
	return out
}

const chatMaxRequestBytes = 512 << 10

func chatOutputTokens(n int) int {
	if n <= 0 {
		return DefaultMaxOutputTokens
	}
	return max(1024, min(n, MaxOutputTokensLimit))
}

// chatJSONObject returns raw when it is a JSON object, else {} so providers never get
// a malformed arguments or response payload.
func chatJSONObject(raw json.RawMessage) json.RawMessage {
	var probe map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &probe) != nil || probe == nil {
		return json.RawMessage(`{}`)
	}
	return raw
}
