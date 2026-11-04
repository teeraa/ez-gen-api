package modelark

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"testing"
)

func usageContext() context.Context {
	return WithUsageOwner(context.Background(), uuid.New(), uuid.New())
}

// The SDK mutates shared session configuration during construction, so provider fixtures run sequentially.
func TestImageUsagePreservesMissingAndZeroTokens(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		hasTokens   bool
	}{
		{"absent", "", false}, {"reported_zero", `,"usage":{"generated_images":1,"output_tokens":0,"total_tokens":0}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var events []UsageEvent
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"model":"actual-model","created":123,"data":[{"b64_json":"Zml4dHVyZQ=="}]%s}`, tc.usage)
			}))
			defer server.Close()
			recorder := func(ctx context.Context, e UsageEvent) error { events = append(events, e); return nil }
			client := NewImageClientWithUsage("fixture", server.URL, "fallback-model", recorder)
			ctx := usageContext()
			for i := 0; i < 2; i++ {
				if _, err := client.GenerateImage(ctx, ImageRequest{Model: "actual-model", Prompt: "private prompt"}); err != nil {
					t.Fatal(err)
				}
			}
			if len(events) != 4 {
				t.Fatalf("events %d", len(events))
			}
			first, last := events[0], events[1]
			if first.Status != "started" || last.Status != "succeeded" || first.AttemptID != last.AttemptID || first.AttemptID == events[2].AttemptID {
				t.Fatalf("attempt lifecycle %+v", events)
			}
			if last.UserID == uuid.Nil || last.JobID == uuid.Nil || last.Model != "actual-model" || last.CompletedAt == nil || last.LatencyMS == nil {
				t.Fatalf("metadata %+v", last)
			}
			if last.InputTokens != nil || (last.OutputTokens != nil) != tc.hasTokens || (last.TotalTokens != nil) != tc.hasTokens {
				t.Fatalf("nullable tokens %+v", last)
			}
			if tc.hasTokens && (*last.OutputTokens != 0 || *last.TotalTokens != 0 || *last.GeneratedImages != 1) {
				t.Fatal("reported zero lost")
			}
		})
	}
}

func TestUsagePersistenceFailurePreventsPaidCall(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer server.Close()
	recorder := func(context.Context, UsageEvent) error { return errors.New("database unavailable") }
	ctx := usageContext()
	if _, err := NewImageClientWithUsage("fixture", server.URL, "model", recorder).GenerateImage(ctx, ImageRequest{}); err == nil {
		t.Fatal("expected persistence error")
	}
	if _, err := NewClientWithUsage("fixture", server.URL, recorder).CreateTask(ctx, CreateTaskRequest{Model: "model"}); err == nil {
		t.Fatal("expected persistence error")
	}
	if calls != 0 {
		t.Fatalf("paid calls %d", calls)
	}
}

func TestFailedSubmissionRetainsUnknownTokens(t *testing.T) {
	var events []UsageEvent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"code":"RateLimitExceeded","message":"private"}}`)
	}))
	defer server.Close()
	recorder := func(ctx context.Context, e UsageEvent) error { events = append(events, e); return nil }
	_, err := NewClientWithUsage("fixture", server.URL, recorder).CreateTask(usageContext(), CreateTaskRequest{Model: "video-actual"})
	if err == nil || len(events) != 2 {
		t.Fatalf("err %v events %d", err, len(events))
	}
	last := events[1]
	if last.Status != "failed" || last.Model != "video-actual" || last.InputTokens != nil || last.OutputTokens != nil || last.TotalTokens != nil || last.CompletedAt == nil {
		t.Fatalf("failed usage %+v", last)
	}
}

func TestVideoUsagePollUpdatesExistingAttempt(t *testing.T) {
	var events []UsageEvent
	poll := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			fmt.Fprint(w, `{"id":"task-1"}`)
			return
		}
		poll++
		if poll == 1 {
			fmt.Fprint(w, `{"id":"task-1","model":"actual-video","status":"running"}`)
			return
		}
		fmt.Fprint(w, `{"id":"task-1","model":"actual-video","status":"succeeded","usage":{"completion_tokens":0,"total_tokens":42},"duration":5}`)
	}))
	defer server.Close()
	recorder := func(ctx context.Context, e UsageEvent) error { events = append(events, e); return nil }
	client := NewClientWithUsage("fixture", server.URL, recorder)
	ctx := usageContext()
	if _, err := client.CreateTask(ctx, CreateTaskRequest{Model: "actual-video"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := client.GetTask(ctx, "task-1"); err != nil {
			t.Fatal(err)
		}
	}
	if len(events) != 4 {
		t.Fatalf("events %d", len(events))
	}
	if events[1].Status != "submitted" || events[1].ProviderTaskID != "task-1" || events[1].CompletedAt != nil {
		t.Fatalf("submission %+v", events[1])
	}
	for _, e := range events[2:] {
		if e.AttemptID != uuid.Nil || e.ProviderTaskID != "task-1" || e.UserID != events[0].UserID || e.JobID != events[0].JobID || e.InputTokens != nil || e.OutputTokens == nil || *e.OutputTokens != 0 || e.TotalTokens == nil || *e.TotalTokens != 42 || *e.DurationSeconds != 5 {
			t.Fatalf("poll must update existing attempt %+v", e)
		}
	}
}

func TestCompletionUsesDetachedBoundedContext(t *testing.T) {
	ctx, cancel := context.WithCancel(usageContext())
	event, err := beginUsage(ctx, func(context.Context, UsageEvent) error { return nil }, "model", "image_generation")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	event.Status = "failed"
	recorded := false
	finishUsage(ctx, func(c context.Context, e UsageEvent) error {
		recorded = true
		if c.Err() != nil {
			t.Fatal("completion inherited cancellation")
		}
		if _, ok := c.Deadline(); !ok {
			t.Fatal("completion has no deadline")
		}
		return nil
	}, event)
	if !recorded {
		t.Fatal("completion missing")
	}
}

func TestCompletionFailureDoesNotDiscardSubmittedTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"paid-task"}`)
	}))
	defer server.Close()
	recorder := func(ctx context.Context, e UsageEvent) error {
		if e.Status != "started" {
			return errors.New("temporary write failure")
		}
		return nil
	}
	id, err := NewClientWithUsage("fixture", server.URL, recorder).CreateTask(usageContext(), CreateTaskRequest{Model: "model"})
	if err != nil || id != "paid-task" {
		t.Fatalf("paid task lost id=%s err=%v", id, err)
	}
}
