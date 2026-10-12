# Skill: integrate-modelark

Add or update BytePlus ModelArk integration code in the VIOLET STUDIO backend.

## When to Use

- Implementing `apps/services/modelark/` for the first time
- Adding a new provider call (e.g., cancel task)
- Updating status mapping when provider API changes
- Debugging a generation failure
- Adding or updating the status poller in `apps/crons/`

---

## Critical Facts (Read Before Starting)

```
Official SDK:  github.com/volcengine/ark-runtime-go (v0.11+)
Base URL:      https://ark.ap-southeast.bytepluses.com/api/v3
Auth env var:  ARK_API_KEY
Task ID field: id (prefix: cgt-...)

Provider statuses: queued → running → succeeded | failed | cancelled
App statuses:      submitted → processing → succeeded | failed | cancelled
                   (queued = before provider call; submitted = after CreateTask succeeds)

Output URL: response.content.video_url — expires ~24h — DOWNLOAD IMMEDIATELY
```

Full research: `docs/engineering/modelark-integration-research.md`

---

## Package Structure

```
apps/services/modelark/
├── client.go      # Interface + SDK wrapper
├── requests.go    # Application → provider request mapping
├── responses.go   # Application-domain result types (NOT SDK types)
└── mapper.go      # Status/error mapping
```

**Rule:** Nothing outside this package should import `ark-runtime-go` types.

---

## Step 1: Define the Application Interface

`apps/services/modelark/client.go`:

```go
package modelark

import "context"

// Client is the interface facades depend on. Mock this in tests.
type Client interface {
    CreateTask(ctx context.Context, req CreateTaskRequest) (taskID string, err error)
    GetTask(ctx context.Context, taskID string) (TaskResult, error)
    CancelTask(ctx context.Context, taskID string) error
}

// Config holds all provider configuration — read from environment.
type Config struct {
    APIKey          string
    BaseURL         string
    DefaultModel    string
    RequestTimeout  time.Duration
    PollInterval    time.Duration
}

func ConfigFromEnv() Config {
    timeout, _ := strconv.Atoi(os.Getenv("ARK_REQUEST_TIMEOUT_SECONDS"))
    if timeout == 0 { timeout = 300 }
    poll, _ := strconv.Atoi(os.Getenv("ARK_POLL_INTERVAL_SECONDS"))
    if poll == 0 { poll = 5 }
    return Config{
        APIKey:         os.Getenv("ARK_API_KEY"),
        BaseURL:        os.Getenv("ARK_BASE_URL"),
        DefaultModel:   os.Getenv("ARK_DEFAULT_MODEL"),
        RequestTimeout: time.Duration(timeout) * time.Second,
        PollInterval:   time.Duration(poll) * time.Second,
    }
}
```

---

## Step 2: Application-Domain Request/Response Types

`apps/services/modelark/requests.go`:
```go
type CreateTaskRequest struct {
    ProviderModelID string       // Resolved from model_configs table
    Prompt          string
    Mode            string       // "text" | "image_video"
    AspectRatio     string       // "16:9" | "9:16" | "1:1"
    DurationSeconds int
    Resolution      string       // "720p" | "1080p"
    EnhancePrompt   bool
    ReferenceImageURL *string    // Only for image_video mode
}
```

`apps/services/modelark/responses.go`:
```go
type TaskResult struct {
    ProviderTaskID string
    Status         TaskStatus    // Application-domain status (not provider string)
    VideoURL       string        // Only when Status == TaskStatusSucceeded
    FailureCode    string        // Only when Status == TaskStatusFailed
    FailureMessage string        // Sanitized — safe to log
}

type TaskStatus int
const (
    TaskStatusSubmitted  TaskStatus = iota
    TaskStatusProcessing
    TaskStatusSucceeded
    TaskStatusFailed
    TaskStatusCancelled
)
```

---

## Step 3: Status Mapper

`apps/services/modelark/mapper.go`:
```go
func mapProviderStatus(s string) TaskStatus {
    switch s {
    case "queued":
        return TaskStatusSubmitted
    case "running":
        return TaskStatusProcessing
    case "succeeded":
        return TaskStatusSucceeded
    case "failed":
        return TaskStatusFailed
    case "cancelled":
        return TaskStatusCancelled
    default:
        // Unknown status — treat as processing to avoid premature failure
        return TaskStatusProcessing
    }
}
```

Table-drive test this function for ALL five provider status strings. Test the unknown case too.

---

## Step 4: SDK Implementation

`apps/services/modelark/client.go` (concrete impl):
```go
type arkClient struct {
    sdk    *arkruntime.Client  // The official SDK client
    config Config
}

func NewClient(cfg Config) (Client, error) {
    if cfg.APIKey == "" {
        return nil, errors.New("ARK_API_KEY is required")
    }
    sdkClient := arkruntime.NewByteplusClientWithApiKey(
        cfg.APIKey,
        option.WithBaseUrl(cfg.BaseURL),
    )
    return &arkClient{sdk: sdkClient, config: cfg}, nil
}

func (c *arkClient) CreateTask(ctx context.Context, req CreateTaskRequest) (string, error) {
    ctx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
    defer cancel()

    // Map application request → SDK request
    sdkReq := mapToSDKRequest(req)

    resp, err := c.sdk.CreateContentGenerationTask(ctx, sdkReq)
    if err != nil {
        return "", fmt.Errorf("modelark create task: %w", err)
    }
    return resp.ID, nil
}
```

---

## Step 5: Media Download on Success

When `GetTask` returns `TaskStatusSucceeded`:
1. Download video from `response.content.video_url`
2. Upload to object storage
3. Return the object storage KEY (not the BytePlus URL) in `TaskResult`

The facade is responsible for triggering this. The storage operation belongs in
`apps/services/storage/` — a separate service the facade calls after `GetTask` succeeds.

---

## Step 6: Cancellation Logic

```go
func (c *arkClient) CancelTask(ctx context.Context, taskID string) error {
    ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()
    err := c.sdk.DeleteContentGenerationTask(ctx, taskID)
    if err != nil {
        // BytePlus returns an error if task is already running
        // Caller must handle this case
        return fmt.Errorf("modelark cancel task %s: %w", taskID, err)
    }
    return nil
}
```

The facade handles the "cannot cancel a running task" case — it does NOT call `CancelTask`
if the application status is `processing`. It marks the job cancelled in the DB directly.

---

## Step 7: Poller Skeleton

`apps/crons/video_generation_poller.go`:
```go
type Poller struct {
    repo     repository.JobRepository
    client   modelark.Client
    storage  storage.Client
    interval time.Duration
}

func (p *Poller) Run(ctx context.Context) {
    ticker := time.NewTicker(p.interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            p.poll(ctx)
        }
    }
}

func (p *Poller) poll(ctx context.Context) {
    jobs, err := p.repo.FindActiveJobs(ctx)
    if err != nil { /* log, continue */ return }
    for _, job := range jobs {
        go p.syncJob(ctx, job)  // Only if safe to do concurrently — verify with architect
    }
}
```

---

## Verify

```bash
go build ./apps/services/modelark/...
go test -race ./apps/services/modelark/...  # Must pass without ARK_API_KEY
go test -race ./apps/crons/...
```

No test should require `ARK_API_KEY` to be set.
