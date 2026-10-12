package tests

import (
	"testing"

	"github.com/ez-gen/ez-gen-api/apps/models"
	"github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

func TestMapProviderStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider modelark.ProviderStatus
		expected models.JobStatus
	}{
		{modelark.ProviderStatusQueued, models.JobStatusSubmitted},
		{modelark.ProviderStatusRunning, models.JobStatusProcessing},
		{modelark.ProviderStatusSucceeded, models.JobStatusSucceeded},
		{modelark.ProviderStatusFailed, models.JobStatusFailed},
		{modelark.ProviderStatusCancelled, models.JobStatusCancelled},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.provider), func(t *testing.T) {
			t.Parallel()
			got := modelark.MapProviderStatus(tc.provider)
			if got != tc.expected {
				t.Errorf("MapProviderStatus(%q) = %q, want %q", tc.provider, got, tc.expected)
			}
		})
	}
}

func TestQualityToResolution(t *testing.T) {
	t.Parallel()
	if modelark.QualityToResolution("high") != "1080p" {
		t.Error("high quality should map to 1080p")
	}
	if modelark.QualityToResolution("standard") != "720p" {
		t.Error("standard quality should map to 720p")
	}
	if modelark.QualityToResolution("") != "720p" {
		t.Error("empty quality should default to 720p")
	}
}

func TestMockClient_CreateAndGet(t *testing.T) {
	t.Parallel()
	mock := modelark.NewMockClient()
	mock.NextTaskID = "cgt-test-abc"

	ctx := t.Context()
	id, err := mock.CreateTask(ctx, modelark.CreateTaskRequest{
		Model:   "dreamina-seedance-2-0-fast-260128",
		Content: []modelark.ContentItem{{Type: modelark.ContentTypeText, Text: "hello"}},
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if id != "cgt-test-abc" {
		t.Errorf("unexpected task id: %q", id)
	}

	// Manually set a result for polling
	mock.SetTask("cgt-test-abc", &modelark.TaskResult{
		ProviderTaskID: "cgt-test-abc",
		Status:         modelark.ProviderStatusSucceeded,
		VideoURL:       "https://provider.example.com/video.mp4",
	})

	result, err := mock.GetTask(ctx, "cgt-test-abc")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if result.Status != modelark.ProviderStatusSucceeded {
		t.Errorf("expected succeeded, got %q", result.Status)
	}
	if result.VideoURL == "" {
		t.Error("expected non-empty video URL")
	}
}

func TestMockClient_GetUnknownTask(t *testing.T) {
	t.Parallel()
	mock := modelark.NewMockClient()
	_, err := mock.GetTask(t.Context(), "cgt-unknown")
	if err == nil {
		t.Error("expected error for unknown task, got nil")
	}
}

func TestJobStatus_IsTerminal(t *testing.T) {
	t.Parallel()
	terminal := []models.JobStatus{
		models.JobStatusSucceeded,
		models.JobStatusFailed,
		models.JobStatusCancelled,
	}
	nonTerminal := []models.JobStatus{
		models.JobStatusQueued,
		models.JobStatusSubmitted,
		models.JobStatusProcessing,
	}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("expected %q to be terminal", s)
		}
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("expected %q to be non-terminal", s)
		}
	}
}
