package modelark

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestVideoSubmissionDoesNotRetryAndDoesNotExposeProviderBody(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"RateLimit","message":"secret-key-and-signed-url"}}`))
	}))
	defer s.Close()
	_, e := NewClient("test-only", s.URL+"/api/v3").CreateTask(context.Background(), CreateTaskRequest{Model: "fixture", Content: []ContentItem{{Type: ContentTypeText, Text: "fixture"}}})
	if e == nil || calls.Load() != 1 {
		t.Fatalf("calls=%d err=%v", calls.Load(), e)
	}
	if strings.Contains(e.Error(), "secret-key") || !errors.Is(e, ErrProviderError) {
		t.Fatalf("unsafe error: %v", e)
	}
	code, _ := VideoFailure(e)
	if code != "PROVIDER_RATE_LIMIT" {
		t.Fatalf("code=%s", code)
	}
}
func TestVideoTimeoutGuidesUsageCheck(t *testing.T) {
	code, message := VideoFailure(context.DeadlineExceeded)
	if code != "PROVIDER_TIMEOUT" || !strings.Contains(message, "ยอดใช้งาน") {
		t.Fatalf("%s %s", code, message)
	}
}
