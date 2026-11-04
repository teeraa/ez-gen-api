package modelark

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/volcengine/ark-runtime-go/arkruntime"
)

type draftTransport func(*http.Request) (*http.Response, error)

func (f draftTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDraftRefinementWireRequest(t *testing.T) {
	var got map[string]json.RawMessage
	transport := draftTransport(func(r *http.Request) (*http.Response, error) {
		if e := json.NewDecoder(r.Body).Decode(&got); e != nil {
			t.Fatal(e)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"fixture-final-task"}`))}, nil
	})
	client := &arkClient{sdk: newSDKClient("fixture-only", arkruntime.WithBaseUrl("https://fixture.invalid/api/v3"), arkruntime.WithHTTPClient(&http.Client{Transport: transport}), arkruntime.WithRetryTimes(0))}
	id, e := client.CreateTask(context.Background(), CreateTaskRequest{Model: "dreamina-seedance-2-5-260628", Content: []ContentItem{{Type: ContentTypeDraftTask, DraftTaskID: "fixture-draft"}}, Resolution: "1080p", ReturnLastFrame: true})
	if e != nil || id != "fixture-final-task" {
		t.Fatalf("id=%q err=%v", id, e)
	}
	for _, field := range []string{"draft", "duration", "ratio", "seed", "generate_audio", "optimize_prompt", "omni_reference_task_type"} {
		if _, ok := got[field]; ok {
			t.Errorf("forbidden final parameter %s present", field)
		}
	}
	var content []struct {
		Type      string `json:"type"`
		DraftTask struct {
			ID string `json:"id"`
		} `json:"draft_task"`
	}
	if json.Unmarshal(got["content"], &content) != nil || len(content) != 1 || content[0].Type != "draft_task" || content[0].DraftTask.ID != "fixture-draft" {
		t.Fatalf("bad draft content %s", got["content"])
	}
	if string(got["resolution"]) != `"1080p"` || string(got["model"]) != `"dreamina-seedance-2-5-260628"` {
		t.Fatalf("wrong model/resolution %v", got)
	}
}

func TestDraftExpiryBoundaryAndResolvedCapability(t *testing.T) {
	cutoff := time.Date(2026, 10, 28, 2, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		created time.Time
		days    int
	}{{cutoff.Add(-time.Second), 7}, {cutoff, 14}, {cutoff.Add(time.Second), 14}} {
		if got := DraftExpiresAt(tc.created); !got.Equal(tc.created.Add(time.Duration(tc.days) * 24 * time.Hour)) {
			t.Fatal(got)
		}
	}
	for _, provider := range []string{"seedance-1-0-pro-250528", "dreamina-seedance-2-5-260628"} {
		registry, e := NewRegistry(provider)
		if e != nil {
			t.Fatal(e)
		}
		for _, internal := range []string{"violet-realism", "violet-motion-2"} {
			m, _ := registry.Lookup(internal)
			if m.SupportsDraft() != (provider == "dreamina-seedance-2-5-260628") {
				t.Fatalf("wrong override eligibility %s %s", internal, provider)
			}
		}
	}
}
