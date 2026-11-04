package modelark

import (
	"context"
	"encoding/base64"
	"encoding/json"
	sdkmodel "github.com/volcengine/ark-runtime-go/arkruntime/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImageClientUsesDedicatedAPIAndNoRetry(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v3/images/generations" {
			t.Errorf("unexpected route %s", r.URL.Path)
		}
		var body struct {
			Model  string `json:"model"`
			Format string `json:"response_format"`
			Size   string `json:"size"`
		}
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		if body.Model != "snapshot-model" || body.Format != "b64_json" || body.Size != "2048x2048" {
			t.Errorf("bad body %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"model": "snapshot-model", "created": 123, "data": []map[string]string{{"b64_json": base64.StdEncoding.EncodeToString([]byte("fixture image bytes"))}}})
	}))
	defer s.Close()
	c := NewImageClient("fixture", s.URL, "fallback")
	data, e := c.GenerateImage(context.Background(), ImageRequest{Model: "snapshot-model", Prompt: "character", Size: "2048x2048"})
	if e != nil || string(data) != "fixture image bytes" || calls != 1 {
		t.Fatalf("data %q error %v calls %d", data, e, calls)
	}
	calls = 0
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"code":"RateLimitExceeded","message":"SECRET_VALUE"}}`))
	}))
	defer fail.Close()
	_, e = NewImageClient("fixture", fail.URL, "model").GenerateImage(context.Background(), ImageRequest{Prompt: "x"})
	if e == nil || strings.Contains(e.Error(), "SECRET_VALUE") || calls != 1 {
		t.Fatalf("error %v calls %d", e, calls)
	}
}
func TestImageErrorCategories(t *testing.T) {
	for _, tc := range []struct {
		status     int
		code, want string
	}{{401, "Auth", "PROVIDER_ACCESS_DENIED"}, {403, "ModelNotActivated", "PROVIDER_ACCESS_DENIED"}, {429, "RateLimit", "PROVIDER_RATE_LIMIT"}, {429, "QuotaExceeded", "PROVIDER_QUOTA"}, {404, "Missing", "MODEL_UNAVAILABLE"}, {400, "InvalidInput", "PROVIDER_INVALID_INPUT"}} {
		e := classifyImageError(&sdkmodel.APIError{HTTPStatusCode: tc.status, Code: tc.code, Message: "SECRET_VALUE"}).(*ImageError)
		if e.Code != tc.want || strings.Contains(e.Error(), "SECRET_VALUE") {
			t.Errorf("%+v got %+v", tc, e)
		}
	}
}

func TestImageClientSerializesReferenceImage(t *testing.T) {
	const reference = "https://storage.test/signed/reference.png?token=fixture"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Image  string `json:"image"`
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.Image != reference || request.Prompt != "preserve referenced identity" {
			t.Errorf("reference request changed %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"model":"snapshot-model","created":123,"data":[{"b64_json":"Zml4dHVyZQ=="}]}`))
	}))
	defer server.Close()
	result, err := NewImageClient("fixture", server.URL, "snapshot-model").GenerateImage(context.Background(), ImageRequest{Model: "snapshot-model", Prompt: "preserve referenced identity", Size: "2048x2048", ReferenceURL: reference})
	if err != nil || string(result) != "fixture" || calls != 1 {
		t.Fatalf("result %q err %v calls %d", result, err, calls)
	}
}
