package editorai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var chatTestTools = []ChatTool{{Name: "get_timeline", Description: "read", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}}

func TestGeminiChatToolRoundTrip(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "k" || !strings.HasSuffix(r.URL.Path, "/models/m:generateContent") {
			t.Fatalf("request %s %v", r.URL.Path, r.Header)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[{"text":"thinking","thought":true},{"functionCall":{"name":"get_timeline","args":{}},"thoughtSignature":"sig"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[{"text":"เสร็จแล้ว"}]}}]}`))
	}))
	defer server.Close()
	g := NewGeminiWithEndpoint("k", "m", "v", server.URL, server.Client())
	turns := []ChatTurn{{Role: "user", Text: "ดู timeline"}}
	reply, err := g.Chat(context.Background(), ChatRequest{System: "s", Turns: turns, Tools: chatTestTools})
	if err != nil || len(reply.ToolCalls) != 1 || reply.ToolCalls[0].Name != "get_timeline" || reply.Text != "" || reply.Usage.InputTokens != 10 {
		t.Fatalf("reply %+v %v", reply, err)
	}
	if bodies[0]["tools"] == nil || bodies[0]["toolConfig"] == nil {
		t.Fatalf("tools missing %v", bodies[0])
	}
	turns = append(turns, reply.Turn(), ChatTurn{Role: "user", ToolResults: []ToolResult{{CallID: reply.ToolCalls[0].ID, Name: "get_timeline", Content: json.RawMessage(`{"segments":[]}`)}}})
	reply, err = g.Chat(context.Background(), ChatRequest{System: "s", Turns: turns, Tools: chatTestTools})
	if err != nil || reply.Text != "เสร็จแล้ว" {
		t.Fatalf("second %+v %v", reply, err)
	}
	contents, _ := json.Marshal(bodies[1]["contents"])
	if !strings.Contains(string(contents), `"thoughtSignature":"sig"`) || !strings.Contains(string(contents), `"functionResponse":{"name":"get_timeline","response":{"segments":[]}}`) {
		t.Fatalf("replay %s", contents)
	}
}

func TestGeminiChatRateLimitAndEmpty(t *testing.T) {
	status := 429
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status == 429 {
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(429)
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[]}}]}`))
	}))
	defer server.Close()
	g := NewGeminiWithEndpoint("k", "m", "v", server.URL, server.Client())
	var rate *RateError
	if _, err := g.Chat(context.Background(), ChatRequest{Turns: []ChatTurn{{Role: "user", Text: "x"}}}); !errors.As(err, &rate) {
		t.Fatalf("want rate error, got %v", err)
	}
	status = 200
	if _, err := g.Chat(context.Background(), ChatRequest{Turns: []ChatTurn{{Role: "user", Text: "x"}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("want invalid, got %v", err)
	}
}

func TestClaudeChatToolRoundTrip(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" || r.Header.Get("anthropic-version") == "" || r.URL.Path != "/messages" {
			t.Fatalf("request %s %v", r.URL.Path, r.Header)
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ขอดูก่อน"},{"type":"tool_use","id":"tu_1","name":"get_timeline","input":{}}],"stop_reason":"tool_use","usage":{"input_tokens":7,"output_tokens":2}}`))
			return
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"เรียบร้อย"}],"stop_reason":"end_turn","usage":{"input_tokens":9,"output_tokens":1}}`))
	}))
	defer server.Close()
	c := NewClaudeWithEndpoint("k", "claude-test", server.URL, server.Client())
	turns := []ChatTurn{{Role: "user", Text: "ดู timeline"}}
	reply, err := c.Chat(context.Background(), ChatRequest{System: "s", Turns: turns, Tools: chatTestTools})
	if err != nil || len(reply.ToolCalls) != 1 || reply.ToolCalls[0].ID != "tu_1" || reply.Text != "ขอดูก่อน" {
		t.Fatalf("reply %+v %v", reply, err)
	}
	turns = append(turns, reply.Turn(), ChatTurn{Role: "user", ToolResults: []ToolResult{{CallID: "tu_1", Name: "get_timeline", Content: json.RawMessage(`{"ok":true}`), Images: []Image{{AssetID: "a", Time: 1, Data: []byte{1}}}}}})
	reply, err = c.Chat(context.Background(), ChatRequest{System: "s", Turns: turns, Tools: chatTestTools})
	if err != nil || reply.Text != "เรียบร้อย" {
		t.Fatalf("second %+v %v", reply, err)
	}
	messages, _ := json.Marshal(bodies[1]["messages"])
	if !strings.Contains(string(messages), `"tool_use_id":"tu_1"`) || !strings.Contains(string(messages), `"type":"image"`) || !strings.Contains(string(messages), `"id":"tu_1"`) {
		t.Fatalf("messages %s", messages)
	}
	if tools, _ := json.Marshal(bodies[0]["tools"]); !strings.Contains(string(tools), `"input_schema"`) {
		t.Fatalf("tools %s", tools)
	}
}

func TestClaudeUnavailableWithoutModel(t *testing.T) {
	if NewClaude("k", "").Available() || NewClaude("", "m").Available() {
		t.Fatal("claude must need key and model")
	}
	if SelectToolCaller("claude", nil, NewClaude("k", "m")).Name() != "claude" || SelectToolCaller("", NewGemini("k", "m", "v"), nil).Name() != "gemini" {
		t.Fatal("select")
	}
}

func TestParseChatModels(t *testing.T) {
	callers := ParseChatModels(" gemini:gemini-3.5-flash-lite, claude:claude-haiku-5-5 ,claude:claude-haiku-5-5,openai:x,claude:,bad", "gk", "v1", "")
	if len(callers) != 2 {
		t.Fatalf("want 2 callers (duplicate, unknown provider, empty model and malformed skipped), got %d", len(callers))
	}
	if ChatModelID(callers[0]) != "gemini:gemini-3.5-flash-lite" || ChatModelID(callers[1]) != "claude:claude-haiku-5-5" {
		t.Fatalf("ids %s %s", ChatModelID(callers[0]), ChatModelID(callers[1]))
	}
	if !callers[0].Available() || callers[1].Available() {
		t.Fatal("a listed model without its provider key stays listed but unavailable")
	}
	if len(ParseChatModels("", "gk", "v1", "ak")) != 0 {
		t.Fatal("empty spec")
	}
}
