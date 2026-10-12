package modelark

import (
	"context"
	"sync"
)

// MockClient is a thread-safe in-memory test double for the Client interface.
// Tests control the response by setting tasks in the store.
type MockClient struct {
	mu    sync.RWMutex
	tasks map[string]*TaskResult
	// CreateErr, if non-nil, is returned by CreateTask.
	CreateErr error
	// CreatedRequests records every CreateTask call for assertion.
	CreatedRequests []CreateTaskRequest
	// NextTaskID is the provider task ID returned by CreateTask.
	NextTaskID string
}

func NewMockClient() *MockClient {
	return &MockClient{
		tasks:      make(map[string]*TaskResult),
		NextTaskID: "cgt-test-task-id",
	}
}

func (m *MockClient) CreateTask(_ context.Context, req CreateTaskRequest) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CreatedRequests = append(m.CreatedRequests, req)
	if m.CreateErr != nil {
		return "", m.CreateErr
	}
	return m.NextTaskID, nil
}

func (m *MockClient) GetTask(_ context.Context, id string) (*TaskResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, ErrTaskNotFound
	}
	cp := *t
	return &cp, nil
}

func (m *MockClient) CancelTask(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return ErrTaskNotFound
	}
	t.Status = ProviderStatusCancelled
	return nil
}

// SetTask registers a canned response for GetTask.
func (m *MockClient) SetTask(id string, result *TaskResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[id] = result
}
