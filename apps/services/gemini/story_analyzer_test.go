package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

func TestStoryAnalyzerAnalyze(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		status     int
		body       string
		want       string
		wantCode   string
		definitive bool
	}{
		{"success skips thoughts", 200, `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"thinking","thought":true},{"text":"{\"logline\":\"x\"}"}]}}]}`, `{"logline":"x"}`, "", false},
		{"bad request rejected", 400, `{}`, "", "ANALYSIS_REJECTED", true},
		{"server error retryable", 503, `{}`, "", "ANALYSIS_UNAVAILABLE", true},
		{"rate limit retryable", 429, `{}`, "", "ANALYSIS_UNAVAILABLE", true},
		{"safety rejected", 200, `{"candidates":[{"finishReason":"SAFETY","content":{"parts":[]}}]}`, "", "ANALYSIS_REJECTED", true},
		{"blocked prompt", 200, `{"promptFeedback":{"blockReason":"SAFETY"}}`, "", "ANALYSIS_REJECTED", true},
		{"truncated output", 200, `{"candidates":[{"finishReason":"MAX_TOKENS","content":{"parts":[{"text":"{"}]}}]}`, "", "ANALYSIS_INVALID_OUTPUT", true},
		{"empty output", 200, `{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"  "}]}}]}`, "", "ANALYSIS_INVALID_OUTPUT", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/models/fixture-model:generateContent" || r.Header.Get("x-goog-api-key") != "k" {
					w.WriteHeader(418)
					return
				}
				raw, _ := io.ReadAll(r.Body)
				var in request
				if json.Unmarshal(raw, &in) != nil || in.Generation.ResponseMIMEType != "application/json" || len(in.Contents) != 1 || in.Contents[0].Parts[0].Text != "PROMPT" {
					w.WriteHeader(418)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			got, err := NewStoryAnalyzerWithEndpoint("k", srv.URL, srv.Client()).Analyze(context.Background(), "fixture-model", "PROMPT")
			if tc.wantCode == "" {
				if err != nil || got != tc.want {
					t.Fatalf("got %q %v", got, err)
				}
				return
			}
			var ae *modelark.StoryAnalysisError
			if !errors.As(err, &ae) || ae.Code != tc.wantCode || ae.Definitive != tc.definitive {
				t.Fatalf("got %v", err)
			}
			if strings.Contains(ae.Message, srv.URL) {
				t.Fatal("error leaked endpoint")
			}
		})
	}
}

func TestStoryAnalyzerNotConfigured(t *testing.T) {
	t.Parallel()
	_, err := NewStoryAnalyzerWithEndpoint("", "http://unused.invalid", http.DefaultClient).Analyze(context.Background(), "m", "p")
	var ae *modelark.StoryAnalysisError
	if !errors.As(err, &ae) || ae.Code != "ANALYSIS_NOT_CONFIGURED" {
		t.Fatalf("got %v", err)
	}
}

func TestMain(m *testing.M) {
	retryBase = time.Millisecond
	os.Exit(m.Run())
}

func TestStoryAnalyzerRetriesTransientFailures(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"overloaded"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"{}"}]}}]}`))
	}))
	defer srv.Close()
	got, err := NewStoryAnalyzerWithEndpoint("k", srv.URL, srv.Client()).Analyze(context.Background(), "m", "p")
	if err != nil || got != "{}" || calls.Load() != 3 {
		t.Fatalf("got %q err %v calls %d", got, err, calls.Load())
	}
}

func TestStoryAnalyzerFailureCarriesLogDetail(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Resource has been exhausted (e.g. check quota)."}}`))
	}))
	defer srv.Close()
	_, err := NewStoryAnalyzerWithEndpoint("k", srv.URL, srv.Client()).Analyze(context.Background(), "m", "p")
	var ae *modelark.StoryAnalysisError
	if !errors.As(err, &ae) || ae.Code != "ANALYSIS_UNAVAILABLE" || !strings.Contains(err.Error(), "http 429") || !strings.Contains(err.Error(), "quota") || strings.Contains(ae.Message, "quota") {
		t.Fatalf("log detail must be in Error(), not the user message: %v", err)
	}
}

func TestStoryAnalyzerDescribeImageSendsInlineImage(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in request
		raw, _ := io.ReadAll(r.Body)
		if json.Unmarshal(raw, &in) != nil || len(in.Contents[0].Parts) != 2 || in.Contents[0].Parts[1].InlineData == nil || in.Contents[0].Parts[1].InlineData.MIMEType != "image/png" || in.Contents[0].Parts[1].InlineData.Data != "iVBO" {
			w.WriteHeader(418)
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"young swordsman in a navy blue robe with gold trim"}]}}]}`))
	}))
	defer srv.Close()
	got, err := NewStoryAnalyzerWithEndpoint("k", srv.URL, srv.Client()).DescribeImage(context.Background(), "m", []byte{0x89, 0x50, 0x4e}, "image/png")
	if err != nil || !strings.Contains(got, "navy blue robe") {
		t.Fatalf("got %q err %v", got, err)
	}
}
