package videoprocessing

import "context"

// MockProcessor returns minimal valid MP4 bytes for testing.
// Real codec output is not needed — tests only verify the facade's orchestration logic.
type MockProcessor struct {
	MergeFunc func(ctx context.Context, req MergeRequest) ([]byte, error)
}

func (m *MockProcessor) Merge(ctx context.Context, req MergeRequest) ([]byte, error) {
	if m.MergeFunc != nil {
		return m.MergeFunc(ctx, req)
	}
	return []byte("mock-video-data"), nil
}
