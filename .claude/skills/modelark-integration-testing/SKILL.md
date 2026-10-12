# Skill: modelark-integration-testing

Write and run tests for the ModelArk integration: provider client mocks,
ContentItem builder, status mapper, Creative DNA compiler, storage mock,
poller behavior, and security properties.

## Scope

This skill covers testing for:
- `apps/services/modelark/` — client, ContentBuilder, mapper, models
- `apps/services/dna/` — prompt compiler
- `apps/services/storage/` — local mock storage
- `apps/crons/` — poller behavior
- `apps/facades/facade_video_generation/` — lifecycle and idempotency

**Does not cover:** handler tests (covered by `add-api-endpoint` skill),
repository integration tests (covered by `database-change` skill).

---

## When to Invoke

- After implementing any component in the above packages
- Before marking a lifecycle task complete
- When a generation flow is broken and cause is unknown (write a reproducing test first)
- When adding a new model capability (test the registry and validator)

---

## Constraint: No Live Provider Calls

```
Tests MUST pass with ARK_API_KEY unset.
Any test that calls BytePlus fails CI.
```

Use a mock HTTP transport or a mock `Client` interface — never the real SDK client.

---

## Mock Infrastructure

### Mock ModelArk Client

`apps/tests/mock_modelark.go`:

```go
package tests

import (
    "context"
    "github.com/ez-gen/ez-gen-api/apps/services/modelark"
)

type MockModelArkClient struct {
    CreateTaskFn func(ctx context.Context, req modelark.CreateTaskRequest) (string, error)
    GetTaskFn    func(ctx context.Context, taskID string) (modelark.TaskResult, error)
    CancelTaskFn func(ctx context.Context, taskID string) error
}

func (m *MockModelArkClient) CreateTask(ctx context.Context, req modelark.CreateTaskRequest) (string, error) {
    if m.CreateTaskFn != nil { return m.CreateTaskFn(ctx, req) }
    return "cgt-test-task-id", nil
}
func (m *MockModelArkClient) GetTask(ctx context.Context, taskID string) (modelark.TaskResult, error) {
    if m.GetTaskFn != nil { return m.GetTaskFn(ctx, taskID) }
    return modelark.TaskResult{Status: modelark.TaskStatusSucceeded, VideoURL: "https://test/video.mp4"}, nil
}
func (m *MockModelArkClient) CancelTask(ctx context.Context, taskID string) error {
    if m.CancelTaskFn != nil { return m.CancelTaskFn(ctx, taskID) }
    return nil
}
```

### Mock Storage Client

`apps/tests/mock_storage.go`:

```go
package tests

import (
    "bytes"
    "context"
    "io"
    "sync"
    "time"
)

// MemoryStorage is a thread-safe in-memory storage mock.
type MemoryStorage struct {
    mu   sync.RWMutex
    data map[string][]byte
}

func NewMemoryStorage() *MemoryStorage { return &MemoryStorage{data: map[string][]byte{}} }

func (s *MemoryStorage) Upload(_ context.Context, key string, r io.Reader, _ string) error {
    b, err := io.ReadAll(r)
    if err != nil { return err }
    s.mu.Lock(); defer s.mu.Unlock()
    s.data[key] = b
    return nil
}

func (s *MemoryStorage) Download(_ context.Context, key string) (io.ReadCloser, error) {
    s.mu.RLock(); defer s.mu.RUnlock()
    b, ok := s.data[key]
    if !ok { return nil, fmt.Errorf("key not found: %s", key) }
    return io.NopCloser(bytes.NewReader(b)), nil
}

func (s *MemoryStorage) SignedURL(_ context.Context, key string, _ time.Duration) (string, error) {
    return "https://mock-storage/" + key, nil
}

func (s *MemoryStorage) Delete(_ context.Context, key string) error {
    s.mu.Lock(); defer s.mu.Unlock()
    delete(s.data, key)
    return nil
}

// HasKey returns true if the storage contains the given key (for assertions).
func (s *MemoryStorage) HasKey(key string) bool {
    s.mu.RLock(); defer s.mu.RUnlock()
    _, ok := s.data[key]
    return ok
}
```

---

## Test Suite: Status Mapper

`apps/services/modelark/mapper_test.go`:

```go
func TestMapProviderStatus(t *testing.T) {
    t.Parallel()
    cases := []struct {
        provider string
        want     TaskStatus
    }{
        {"queued",    TaskStatusSubmitted},
        {"running",   TaskStatusProcessing},
        {"succeeded", TaskStatusSucceeded},
        {"failed",    TaskStatusFailed},
        {"cancelled", TaskStatusCancelled},
        {"unknown_value", TaskStatusProcessing}, // unknown → treat as processing
    }
    for _, tc := range cases {
        tc := tc
        t.Run(tc.provider, func(t *testing.T) {
            t.Parallel()
            assert.Equal(t, tc.want, mapProviderStatus(tc.provider))
        })
    }
}
```

---

## Test Suite: ContentBuilder

`apps/services/modelark/content_builder_test.go`:

```go
func TestContentBuilder_TextOnly(t *testing.T) {
    t.Parallel()
    cap := modelRegistry["violet-motion-2"]
    b := NewContentBuilder(cap)
    b.SetPrompt("a sunset over mountains")
    items, err := b.Build()
    require.NoError(t, err)
    require.Len(t, items, 1)
    assert.Equal(t, "text", items[0].Type)
    assert.Equal(t, "a sunset over mountains", items[0].Text)
}

func TestContentBuilder_EmptyPromptReturnsError(t *testing.T) {
    t.Parallel()
    cap := modelRegistry["violet-motion-2"]
    b := NewContentBuilder(cap)
    _, err := b.Build()
    assert.ErrorContains(t, err, "non-empty text item")
}

func TestContentBuilder_LastFrameWithoutFirstFrameReturnsError(t *testing.T) {
    t.Parallel()
    cap := modelRegistry["violet-motion-2"]
    b := NewContentBuilder(cap)
    b.SetPrompt("test")
    err := b.SetLastFrame("https://example.com/frame.jpg")
    assert.ErrorContains(t, err, "first_frame")
}

func TestContentBuilder_ReferenceImageLimitEnforced(t *testing.T) {
    t.Parallel()
    cap := modelRegistry["violet-motion-2"] // MaxReferenceImages = 9
    b := NewContentBuilder(cap)
    b.SetPrompt("test")
    for i := 0; i < cap.MaxReferenceImages; i++ {
        require.NoError(t, b.AddReferenceImage(fmt.Sprintf("https://example.com/ref%d.jpg", i)))
    }
    err := b.AddReferenceImage("https://example.com/overflow.jpg")
    assert.ErrorContains(t, err, "at most")
}
```

---

## Test Suite: Model Registry

`apps/services/modelark/models_test.go`:

```go
func TestModelRegistry_AllModelsPresent(t *testing.T) {
    t.Parallel()
    expected := []string{"violet-motion-2", "violet-cinema", "violet-fast", "violet-realism"}
    for _, id := range expected {
        _, ok := Lookup(id)
        assert.True(t, ok, "model %s missing from registry", id)
    }
}

func TestValidateRequest_InvalidMode(t *testing.T) {
    t.Parallel()
    errs := ValidateRequest("violet-motion-2", "image_video", "16:9", 5, false)
    // violet-motion-2 supports image_video — should pass
    assert.Empty(t, errs)
}

func TestValidateRequest_AudioOnNonAudioModel(t *testing.T) {
    t.Parallel()
    errs := ValidateRequest("violet-motion-2", "text", "16:9", 5, true)
    assert.Contains(t, errs, "model does not support audio generation")
}

func TestValidateRequest_DurationOutOfRange(t *testing.T) {
    t.Parallel()
    errs := ValidateRequest("violet-motion-2", "text", "16:9", 20, false)
    // violet-motion-2 max duration = 15s
    assert.NotEmpty(t, errs)
}
```

---

## Test Suite: DNA Compiler

`apps/services/dna/compiler_test.go`:

```go
func TestCompile_PromptOrder(t *testing.T) {
    t.Parallel()
    out, err := Compile(CompileInput{
        UserPrompt: "walking through rain",
        Style:      &StyleDNA{PromptPrefix: "cinematic 35mm"},
        Characters: []CharacterDNA{{PromptAnchor: "a woman with silver hair"}},
        SceneInstructions: "slow dolly forward",
        ModelCap:   modelRegistry["violet-motion-2"],
    })
    require.NoError(t, err)
    assert.Contains(t, out.CompiledPrompt, "cinematic 35mm")
    // Style must come before user prompt
    stylePos := strings.Index(out.CompiledPrompt, "cinematic 35mm")
    promptPos := strings.Index(out.CompiledPrompt, "walking through rain")
    assert.Less(t, stylePos, promptPos, "style prefix must precede user prompt")
}

func TestCompile_EmptyPromptReturnsError(t *testing.T) {
    t.Parallel()
    _, err := Compile(CompileInput{ModelCap: modelRegistry["violet-motion-2"]})
    assert.Error(t, err)
}

func TestCompile_TruncationRecorded(t *testing.T) {
    t.Parallel()
    longPrompt := strings.Repeat("a", 2500)
    out, err := Compile(CompileInput{
        UserPrompt: longPrompt,
        ModelCap:   modelRegistry["violet-motion-2"],
    })
    require.NoError(t, err)
    assert.LessOrEqual(t, len(out.CompiledPrompt), 2000)
    assert.Contains(t, out.TruncatedLayers, "compiled_prompt")
}
```

---

## Test Suite: Facade Lifecycle

`apps/facades/facade_video_generation/lifecycle_test.go`:

```go
func TestCreateJob_IdempotencyKey_ReturnExisting(t *testing.T) {
    t.Parallel()
    key := "idem-key-001"
    repo := &MockJobRepository{
        FindByIdempotencyKeyFn: func(ctx context.Context, userID, ikey string) (*models.VideoGenerationJob, error) {
            if ikey == key { return &models.VideoGenerationJob{Status: "queued"}, nil }
            return nil, ErrNotFound
        },
    }
    f := newTestFacade(repo, nil, nil)
    result, err := f.CreateJob(ctx, "user-1", CreateJobRequest{
        Prompt: "test", Mode: "text", Model: "violet-motion-2",
        AspectRatio: "16:9", Duration: 5, Quality: "standard",
        IdempotencyKey: &key,
    })
    require.NoError(t, err)
    assert.Equal(t, "queued", result.Status)
}

func TestCreateJob_ModelValidationFails(t *testing.T) {
    t.Parallel()
    f := newTestFacade(nil, nil, nil)
    _, err := f.CreateJob(ctx, "user-1", CreateJobRequest{
        Prompt: "test", Mode: "text", Model: "nonexistent-model",
        AspectRatio: "16:9", Duration: 5, Quality: "standard",
    })
    var ve *ValidationError
    assert.ErrorAs(t, err, &ve)
}
```

---

## Running Tests

```bash
# All ModelArk service tests (no credentials required)
go test -race ./apps/services/modelark/...
go test -race ./apps/services/dna/...
go test -race ./apps/services/storage/...

# Facade tests (no DB required — uses mock repo)
go test -race ./apps/facades/facade_video_generation/...

# Cron tests
go test -race ./apps/crons/...

# All at once
go test -race ./apps/services/... ./apps/facades/... ./apps/crons/...
```

All must pass without any environment variables set.

---

## Acceptance Criteria

- [ ] Status mapper: all 5 provider statuses + unknown case covered
- [ ] ContentBuilder: empty prompt, last_frame without first_frame, limit exceeded
- [ ] ModelRegistry: all 4 internal models present with confirmed provider IDs
- [ ] DNA compiler: prompt order, truncation, empty prompt error
- [ ] Facade: idempotency key returns existing job, model validation error path
- [ ] No test requires `ARK_API_KEY` to be set
- [ ] `go test -race ./apps/...` passes

---

## Related Skills

- `integrate-modelark` — defines the Client interface being mocked
- `modelark-provider-integration` — defines ContentBuilder being tested
- `creative-dna-prompt-compiler` — defines Compile() being tested
- `video-generation-lifecycle` — facade behavior being tested
- `reference-assets-and-storage` — MemoryStorage mock used across test suites
