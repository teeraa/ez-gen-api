package modelark

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestSDKConcurrentConstructorsAndInference(t *testing.T) {
	// Parallel constructor calls exercise the SDK's shared hidden session state,
	// while requests exercise the separate per-client inference transports.
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"fixture-task"}`))
	}))
	defer server.Close()
	var wg sync.WaitGroup
	failures := make(chan error, 24)
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := NewClient("fixture-only", server.URL+"/api/v3")
			_ = NewImageClient("fixture-only", server.URL, "fixture-image")
			_ = NewReferenceAnalyzer("fixture-only", server.URL)
			_, e := client.CreateTask(context.Background(), CreateTaskRequest{Model: "fixture-model"})
			if e != nil {
				failures <- e
			}
		}()
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		t.Error(e)
	}
}
