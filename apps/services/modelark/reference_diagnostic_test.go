package modelark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReferenceAnalyzerThinkingBudget(t *testing.T) {
	for _, model := range []string{"seed-2-0-mini-260428", "unknown-vision"} {
		a := fakeReferenceAnalyzer(func(r *http.Request) (*http.Response, error) {
			var body struct {
				MaxTokens int `json:"max_tokens"`
				Thinking  *struct {
					Type string `json:"type"`
				} `json:"thinking"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.MaxTokens != 512 {
				t.Fatal("budget changed")
			}
			if model == "seed-2-0-mini-260428" {
				if body.Thinking == nil || body.Thinking.Type != "disabled" {
					t.Fatal("thinking must be disabled for bounded description")
				}
			} else if body.Thinking != nil {
				t.Fatal("unsupported thinking injected")
			}
			return referenceResponse("Dark hair and a white shirt.", "stop"), nil
		})
		if _, err := a.AnalyzeReference(context.Background(), model, "https://storage.test/private"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReferenceProviderDiagnosticsThroughSDK(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{{400, "REFERENCE_ANALYSIS_INVALID_INPUT"}, {401, "REFERENCE_ANALYSIS_ACCESS_DENIED"}, {403, "REFERENCE_ANALYSIS_ACCESS_DENIED"}, {404, "REFERENCE_ANALYSIS_MODEL_UNAVAILABLE"}, {429, "REFERENCE_ANALYSIS_RATE_LIMIT"}} {
		a := fakeReferenceAnalyzer(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"code":"InvalidParameter.secret","param":"max_tokens","message":"private image https://secret.test","request_id":"secret-id"}}`))}, nil
		})
		_, err := a.AnalyzeReference(context.Background(), "vision", "https://storage.test/private")
		code, message, d := ReferenceFailure(fmt.Errorf("private wrapper: %w", err))
		if code != tc.code || d.HTTPStatus != tc.status || d.ProviderCode != "InvalidParameter" || d.Param != "max_tokens" || d.Stage != "provider" {
			t.Fatalf("%s %+v", code, d)
		}
		for _, private := range []string{"secret", "https://", "private"} {
			if strings.Contains(fmt.Sprintf("%s %+v", message, d), private) {
				t.Fatal("unsafe diagnostic")
			}
		}
	}
}

func TestReferenceOutputCategories(t *testing.T) {
	for _, tc := range []struct{ text, reason, code string }{{"shirt", "length", "REFERENCE_ANALYSIS_OUTPUT_LIMIT"}, {"", "stop", "REFERENCE_ANALYSIS_EMPTY_OUTPUT"}, {"Sorry, I cannot describe this", "stop", "REFERENCE_ANALYSIS_REFUSED"}, {"", "content_filter", "REFERENCE_ANALYSIS_REFUSED"}, {"secret", "secret-finish", "REFERENCE_ANALYSIS_INVALID_OUTPUT"}} {
		a := fakeReferenceAnalyzer(func(*http.Request) (*http.Response, error) { return referenceResponse(tc.text, tc.reason), nil })
		_, err := a.AnalyzeReference(context.Background(), "vision", "https://storage.test/private")
		code, message, d := ReferenceFailure(err)
		if code != tc.code || d.ContentBytes != len(tc.text) || strings.Contains(message, "secret") || strings.Contains(d.FinishReason, "secret") {
			t.Fatalf("%s %+v", code, d)
		}
	}
	param := "messages[1].content[0].image_url.url"
	if safeReferenceParam(&param) != param {
		t.Fatal("safe path lost")
	}
	param = "messages[1].content[0].image_url.url\nsecret"
	if safeReferenceParam(&param) != "" {
		t.Fatal("unsafe path accepted")
	}
	if code, _, _ := ReferenceFailure(context.DeadlineExceeded); code != "REFERENCE_ANALYSIS_TIMEOUT" {
		t.Fatal(code)
	}
}
