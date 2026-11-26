package claude

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

func TestStoryAnalyzerAnalyze(t *testing.T) {
	retryBase = time.Millisecond
	cases := []struct {
		name, body, want, wantCode string
		status                     int
	}{
		{"plain json", `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"logline\":\"x\"}"}]}`, `{"logline":"x"}`, "", 200},
		{"fenced json", "{\"stop_reason\":\"end_turn\",\"content\":[{\"type\":\"text\",\"text\":\"```json\\n{\\\"a\\\":1}\\n```\"}]}", `{"a":1}`, "", 200},
		{"sentence around json", `{"stop_reason":"end_turn","content":[{"type":"text","text":"Here: {\"a\":[1,2]} done"}]}`, `{"a":[1,2]}`, "", 200},
		{"no json", `{"stop_reason":"end_turn","content":[{"type":"text","text":"sorry"}]}`, "", "ANALYSIS_INVALID_OUTPUT", 200},
		{"cut by max tokens", `{"stop_reason":"max_tokens","content":[{"type":"text","text":"{"}]}`, "", "ANALYSIS_INVALID_OUTPUT", 200},
		{"refusal", `{"stop_reason":"refusal","content":[]}`, "", "ANALYSIS_REJECTED", 200},
		{"bad request", `{}`, "", "ANALYSIS_REJECTED", 400},
		{"overloaded", `{}`, "", "ANALYSIS_UNAVAILABLE", 529},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				var in request
				if r.URL.Path != "/messages" || r.Header.Get("x-api-key") != "k" || json.Unmarshal(raw, &in) != nil || in.Model != "m" || in.MaxTokens != planOutputTokens || in.Messages[0].Content[0].Text != "PROMPT" {
					w.WriteHeader(418)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			got, err := NewStoryAnalyzerWithEndpoint("k", srv.URL, srv.Client()).Analyze(context.Background(), "m", "PROMPT")
			if tc.wantCode == "" {
				if err != nil || got != tc.want {
					t.Fatalf("got %q %v", got, err)
				}
				return
			}
			var se *modelark.StoryAnalysisError
			if !errors.As(err, &se) || se.Code != tc.wantCode {
				t.Fatalf("err = %v, want %s", err, tc.wantCode)
			}
		})
	}
}

func TestStoryAnalyzerDescribeImage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var in request
		if json.Unmarshal(raw, &in) != nil || len(in.Messages[0].Content) != 2 || in.Messages[0].Content[0].Source == nil || in.Messages[0].Content[0].Source.MediaType != "image/png" {
			w.WriteHeader(418)
			return
		}
		_, _ = w.Write([]byte(`{"stop_reason":"end_turn","content":[{"type":"text","text":"A tall swordsman in a white robe."}]}`))
	}))
	defer srv.Close()
	a := NewStoryAnalyzerWithEndpoint("k", srv.URL, srv.Client())
	got, err := a.DescribeImage(context.Background(), "m", []byte{1, 2, 3}, "image/png")
	if err != nil || got != "A tall swordsman in a white robe." {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := a.DescribeImage(context.Background(), "m", []byte{1}, "image/tiff"); err == nil {
		t.Fatal("tiff should be refused")
	}
}
