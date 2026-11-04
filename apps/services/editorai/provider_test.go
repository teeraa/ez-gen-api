package editorai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGeminiStructuredBoundaryAndQuota(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if r.Header.Get("x-goog-api-key") != "private-test-key" || strings.Contains(r.URL.String(), "private-test-key") {
			t.Error("key boundary")
		}
		var input struct {
			Generation struct {
				Max    int `json:"maxOutputTokens"`
				Format struct {
					Text struct {
						MIME   string          `json:"mimeType"`
						Schema json.RawMessage `json:"schema"`
					} `json:"text"`
				} `json:"responseFormat"`
			} `json:"generationConfig"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || input.Generation.Max != 8192 || input.Generation.Format.Text.MIME != "APPLICATION_JSON" || !json.Valid(input.Generation.Format.Text.Schema) {
			t.Error("structured schema")
		}
		if n == 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"{\"ok\":true}"}]}}]}`))
	}))
	defer server.Close()
	g := NewGeminiWithEndpoint("private-test-key", "gemini-3.5-flash-lite", "test-v1", server.URL, server.Client())
	raw, e := g.Generate(context.Background(), "system", "data", nil, json.RawMessage(`{"type":"object"}`))
	if e != nil || string(raw) != `{"ok":true}` {
		t.Fatal(string(raw), e)
	}
	_, e = g.Generate(context.Background(), "system", "data", nil, json.RawMessage(`{"type":"object"}`))
	var rate *RateError
	if !errors.As(e, &rate) || time.Until(rate.RetryAt) <= 0 || calls.Load() != 2 {
		t.Fatal("quota must not retry automatically", e, calls.Load())
	}
	if _, e = g.Generate(context.Background(), "system", strings.Repeat("x", 97<<10), nil, nil); !errors.Is(e, ErrTooLarge) || calls.Load() != 2 {
		t.Fatal("input budget", e)
	}
}
func TestGeminiInvalidAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad/models/m:generateContent" {
			_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"MAX_TOKENS","content":{"parts":[{"text":"{}"}]}}]}`))
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer server.Close()
	g := NewGeminiWithEndpoint("k", "m", "v", server.URL+"/bad", server.Client())
	if _, e := g.Generate(context.Background(), "s", "d", nil, json.RawMessage(`{}`)); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
	g.endpoint = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, e := g.Generate(ctx, "s", "d", nil, json.RawMessage(`{}`)); e == nil || time.Since(start) > time.Second {
		t.Fatal("context not respected", e)
	}
}

func TestGeminiEditorDurationPromptSignature(t *testing.T) {
	t.Parallel()
	provider := NewGemini("", "model", "version")
	if got, want := provider.Signature(), "gemini:model:version:editor-prompts-v3:frames512x3-v1"; got != want {
		t.Fatalf("signature=%q, want %q", got, want)
	}
}
