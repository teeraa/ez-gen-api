package modelark

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/volcengine/ark-runtime-go/arkruntime"
)

type referenceRoundTrip func(*http.Request) (*http.Response, error)

func (f referenceRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fakeReferenceAnalyzer(f referenceRoundTrip) *referenceAnalyzer {
	return &referenceAnalyzer{sdk: newSDKClient("test-secret", arkruntime.WithBaseUrl("https://provider.example/api/v3"), arkruntime.WithRetryTimes(0), arkruntime.WithHTTPClient(&http.Client{Transport: referenceAnalysisTransport{f}}))}
}

func referenceResponse(text, reason string) *http.Response {
	body, _ := json.Marshal(map[string]interface{}{"id": "chat-test", "model": "vision", "created": 1, "object": "chat.completion", "choices": []interface{}{map[string]interface{}{"index": 0, "logprobs": nil, "finish_reason": reason, "message": map[string]string{"role": "assistant", "content": text}}}})
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}
}

func TestReferenceAnalyzerVisionRequest(t *testing.T) {
	calls := 0
	imageURL := "https://storage.example/ref.png?signature=private"
	a := fakeReferenceAnalyzer(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/api/v3/chat/completions" || r.Method != "POST" {
			t.Fatalf("unexpected route %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
			Messages  []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "configured-vision-model" || body.MaxTokens != 512 || len(body.Messages) != 2 {
			t.Fatalf("unexpected model or bounds")
		}
		for _, detail := range []string{"Face (", "Eyes and brows", "Nose and mouth", "Hair (", "Build (", "Clothing and accessories", "Palette", "Visual style", "unknown", "180 words", "untrusted"} {
			if !strings.Contains(string(body.Messages[0].Content), detail) {
				t.Fatalf("missing analysis detail %q", detail)
			}
		}
		if body.Messages[0].Role != "system" || strings.Contains(string(body.Messages[0].Content), imageURL) {
			t.Fatal("reference entered trusted instruction")
		}
		var parts []struct {
			Type     string `json:"type"`
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		if err := json.Unmarshal(body.Messages[1].Content, &parts); err != nil {
			t.Fatal(err)
		}
		if len(parts) != 1 || parts[0].Type != "image_url" || parts[0].ImageURL.URL != imageURL {
			t.Fatal("missing multimodal reference")
		}
		return referenceResponse("  Dark hair.\nWhite shirt.\u202e ", "stop"), nil
	})
	text, err := a.AnalyzeReference(context.Background(), "configured-vision-model", imageURL)
	if err != nil || text != "Dark hair. White shirt." || calls != 1 {
		t.Fatalf("text=%q err=%v calls=%d", text, err, calls)
	}
}

func TestReferenceAnalyzerRejectsInvalidResults(t *testing.T) {
	for _, tc := range []struct{ name, text, reason string }{
		{"empty", " ", "stop"}, {"controls", "\x00\u202e", "stop"}, {"refused", "Sorry, I cannot describe this", "stop"}, {"markup", "<image>shirt</image>", "stop"}, {"long", strings.Repeat("x", 2001), "stop"},
		{"words", strings.Repeat("a ", 201), "stop"}, {"url", "Visit https://private.example", "stop"},
		{"truncated", "A shirt", "length"}, {"moderated", "A shirt", "content_filter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := fakeReferenceAnalyzer(func(*http.Request) (*http.Response, error) { return referenceResponse(tc.text, tc.reason), nil })
			if _, err := a.AnalyzeReference(context.Background(), "vision", "https://storage.example/a"); err == nil {
				t.Fatal("invalid output accepted")
			}
		})
	}
}

func TestReferenceAnalyzerErrorsAreSanitized(t *testing.T) {
	a := fakeReferenceAnalyzer(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("test-secret https://storage.example/private")
	})
	_, err := a.AnalyzeReference(context.Background(), "vision", "https://storage.example/a")
	if err == nil || strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), "storage.example") {
		t.Fatalf("unsafe error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.AnalyzeReference(ctx, "vision", "https://storage.example/a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestReferenceAnalyzerValidatesBeforeCalling(t *testing.T) {
	a := fakeReferenceAnalyzer(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected provider call"); return nil, nil })
	for _, tc := range []struct{ model, url string }{{"", "https://storage.example/a"}, {"vision", "http://storage.example/a"}, {"vision", "https://user:password@storage.example/a"}, {"vision", "file:///private/a"}} {
		if _, err := a.AnalyzeReference(context.Background(), tc.model, tc.url); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
}

func TestReferenceAnalyzerBoundsResponse(t *testing.T) {
	a := fakeReferenceAnalyzer(func(*http.Request) (*http.Response, error) {
		return referenceResponse(strings.Repeat("x", 100000), "stop"), nil
	})
	if _, err := a.AnalyzeReference(context.Background(), "vision", "https://storage.example/a"); err == nil {
		t.Fatal("oversize body accepted")
	}
}

func TestReferenceAnalyzerUsageFailsClosed(t *testing.T) {
	a := fakeReferenceAnalyzer(func(*http.Request) (*http.Response, error) {
		t.Fatal("provider called without usage record")
		return nil, nil
	})
	a.recorder = func(context.Context, UsageEvent) error { return errors.New("cannot persist") }
	if _, err := a.AnalyzeReference(context.Background(), "vision", "https://storage.example/a"); err == nil {
		t.Fatal("missing owner accepted")
	}
}

func TestLegacyReferenceInstructionRemainsAvailable(t *testing.T) {
	t.Parallel()
	a := fakeReferenceAnalyzer(func(r *http.Request) (*http.Response, error) {
		// User content is multimodal, so inspect the trusted message independently.
		var raw struct {
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatal(err)
		}
		var system struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(raw.Messages[0], &system); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(system.Content, "at most 150 words") || strings.Contains(system.Content, "Eyes and brows") {
			t.Fatal("legacy semantics changed")
		}
		return referenceResponse("Silver hair, round glasses.", "stop"), nil
	})
	if _, err := a.AnalyzeReferenceLegacy(context.Background(), "vision", "https://storage.test/ref"); err != nil {
		t.Fatal(err)
	}
}
