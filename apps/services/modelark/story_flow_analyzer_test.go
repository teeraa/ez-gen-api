package modelark

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/volcengine/ark-runtime-go/arkruntime"
	"io"
	"net/http"
	"strings"
	"testing"
)

type storyAnalyzerRoundTrip func(*http.Request) (*http.Response, error)

func (f storyAnalyzerRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func storyAnalyzerFixture(transport storyAnalyzerRoundTrip) *storyFlowAnalyzer {
	return &storyFlowAnalyzer{sdk: newSDKClient("fixture-private-api-key", arkruntime.WithBaseUrl("https://provider.example/api/v3"), arkruntime.WithRetryTimes(0), arkruntime.WithHTTPClient(&http.Client{Transport: storyFlowTransport{transport}}))}
}
func storyAnalysisResponse(status int, content, reason string) *http.Response {
	var body []byte
	if status != 200 {
		body, _ = json.Marshal(map[string]any{"error": map[string]string{"code": "InvalidParameter.private-provider-detail", "message": "private-secret-token private-user-prompt https://private.example/?signature=private"}})
	} else {
		body, _ = json.Marshal(map[string]any{"id": "fixture-chat", "object": "chat.completion", "model": "fixture-analysis", "created": 1, "choices": []any{map[string]any{"index": 0, "logprobs": nil, "finish_reason": reason, "message": map[string]string{"role": "assistant", "content": content}}}})
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}
}
func TestStoryFlowAnalyzerClassifiesSDKResponses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name            string
		status          int
		content, reason string
		transportErr    error
		code            string
		definitive      bool
	}{
		{"bad_request", 400, "", "", nil, "ANALYSIS_REJECTED", true},
		{"unauthorized", 401, "", "", nil, "ANALYSIS_REJECTED", true},
		{"forbidden", 403, "", "", nil, "ANALYSIS_REJECTED", true},
		{"rate_limit", 429, "", "", nil, "ANALYSIS_REJECTED", true},
		{"server_failure", 500, "", "", nil, "ANALYSIS_UNCERTAIN", false},
		{"timeout", 0, "", "", context.DeadlineExceeded, "ANALYSIS_UNCERTAIN", false},
		{"network", 0, "", "", errors.New("private transport key private-user-prompt"), "ANALYSIS_UNCERTAIN", false},
		{"length", 200, "{unfinished", "length", nil, "ANALYSIS_INVALID_OUTPUT", true},
		{"content_filter", 200, "private-user-prompt", "content_filter", nil, "ANALYSIS_REJECTED", true},
		{"empty", 200, "  ", "stop", nil, "ANALYSIS_INVALID_OUTPUT", true},
		{"tool_output", 200, "{}", "tool_calls", nil, "ANALYSIS_INVALID_OUTPUT", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			a := storyAnalyzerFixture(func(r *http.Request) (*http.Response, error) {
				calls++
				if tc.transportErr != nil {
					return nil, tc.transportErr
				}
				return storyAnalysisResponse(tc.status, tc.content, tc.reason), nil
			})
			_, err := a.Analyze(context.Background(), "configured-text-model", "private-user-prompt")
			var failure *StoryAnalysisError
			if !errors.As(err, &failure) || failure.Code != tc.code || failure.Definitive != tc.definitive || calls != 1 {
				t.Fatalf("failure=%v calls=%d", err, calls)
			}
			serialized, _ := json.Marshal(failure)
			for _, secret := range []string{"private-secret-token", "private-user-prompt", "private-api-key", "private-provider-detail", "https://private.example"} {
				if strings.Contains(err.Error(), secret) || strings.Contains(string(serialized), secret) {
					t.Fatal("analysis error disclosed private content")
				}
			}
		})
	}
}
func TestStoryFlowAnalyzerUsesConfiguredTextModel(t *testing.T) {
	t.Parallel()
	a := storyAnalyzerFixture(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.Path != "/api/v3/chat/completions" {
			t.Fatal("not text/chat analysis route")
		}
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Fatal(e)
		}
		if body.Model != "configured-text-model" || len(body.Messages) != 2 || body.Messages[1].Content != "frozen prompt" || body.Messages[0].Role != "system" {
			t.Fatal("configured model/frozen prompt changed")
		}
		return storyAnalysisResponse(200, `{"scenes":[]}`, "stop"), nil
	})
	text, e := a.Analyze(context.Background(), "configured-text-model", "frozen prompt")
	if e != nil || text != `{"scenes":[]}` {
		t.Fatalf("text=%q err=%v", text, e)
	}
}
