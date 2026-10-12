# Go Conventions — VIOLET STUDIO Backend

Rules that apply to all Go code in `ez-gen-api`. These supplement the standard Go community
conventions — follow both; when they conflict, these project rules take precedence.

---

## Error Handling

```go
// CORRECT: wrap with context at every layer boundary
result, err := repo.FindJob(ctx, id)
if err != nil {
    return nil, fmt.Errorf("get job %s: %w", id, err)
}

// WRONG: return bare error without context
return nil, err

// WRONG: ignore errors
repo.DeleteJob(ctx, id)
```

### Sentinel Errors

Define sentinel errors in the domain package. Use `errors.Is()` to check them:

```go
// apps/facades/facade_video_generation/errors.go
var (
    ErrNotFound    = errors.New("not found")
    ErrUnauthorized = errors.New("unauthorized")
    ErrConflict    = errors.New("conflict")
    ErrRateLimit   = errors.New("rate limit exceeded")
)

// Handler usage:
if errors.Is(err, facade.ErrNotFound) { ... }
```

Never compare errors by string: `err.Error() == "not found"` is wrong.

---

## Context

Context must be threaded through all I/O calls:

```go
// CORRECT
func (r *repo) FindJob(ctx context.Context, id string) (*models.VideoGenerationJob, error) {
    var job models.VideoGenerationJob
    result := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&job)
    ...
}

// WRONG: ignoring context
result := r.db.Where("id = ?", id).First(&job)
```

---

## Interfaces

Define interfaces in the **consuming** package, not the implementing package:

```go
// CORRECT: interface in facade package (consumer)
// apps/facades/facade_video_generation/facade.go
type jobRepository interface {
    Create(ctx context.Context, job *models.VideoGenerationJob) error
    FindByID(ctx context.Context, id, userID string) (*models.VideoGenerationJob, error)
}

// WRONG: interface in repository package (producer)
// apps/repositories/repository_video_generation/interface.go — don't put it here
```

Exception: when multiple consumers need the same interface shape, put it in a shared package.

---

## Structs and Constructors

Use constructor functions, not struct literals:

```go
// CORRECT
func NewHandler(facade FacadeInterface, validator *validator.Validate) *Handler {
    return &Handler{facade: facade, validator: validator}
}

// WRONG
h := &Handler{}
h.facade = facade
```

Never export struct fields that are dependencies — use unexported fields + constructor:

```go
type Handler struct {
    facade    FacadeInterface  // unexported
    validator *validator.Validate
}
```

---

## Naming

| Element | Convention | Example |
|---|---|---|
| Package | lowercase, single word | `modelark`, `dto_video_generation` |
| Interface | `<Role>` or `<Action>er` | `JobRepository`, `ModelArkClient` |
| Constructor | `New<Type>` | `NewHandler`, `NewFacade` |
| Error sentinel | `Err<Condition>` | `ErrNotFound`, `ErrConflict` |
| Context param | always `ctx` | `func Foo(ctx context.Context, ...)` |
| DB param | `db` | `func NewRepo(db *gorm.DB) *repo` |
| Test mock | `Mock<Interface>` | `MockJobRepository` |

---

## Comments

Write comments only when the WHY is non-obvious:

```go
// CORRECT: explains a non-obvious constraint
// BytePlus output URLs expire in ~24h. Download immediately to avoid
// serving broken media links to users.
if err := s.storage.Upload(ctx, videoURL, jobID); err != nil { ... }

// WRONG: explains what the code already says clearly
// Find the job by ID
job, err := repo.FindByID(ctx, id)
```

No multi-line comment blocks. No `// TODO` in committed code — create a task instead.

---

## Testing

```go
// Use t.Parallel() in all unit tests
func TestCreateJob_Success(t *testing.T) {
    t.Parallel()
    // ...
}

// Table-driven tests for validation
func TestValidateCreateJobRequest(t *testing.T) {
    t.Parallel()
    cases := []struct {
        name    string
        input   CreateJobRequest
        wantErr bool
    }{
        {"valid", CreateJobRequest{Prompt: "test", ...}, false},
        {"empty prompt", CreateJobRequest{Prompt: "", ...}, true},
    }
    for _, tc := range cases {
        tc := tc  // capture range variable
        t.Run(tc.name, func(t *testing.T) {
            t.Parallel()
            // ...
        })
    }
}
```

---

## GORM Conventions

```go
// Always use WithContext
db.WithContext(ctx).Find(&jobs)

// Ownership filter on every user-scoped query
db.WithContext(ctx).Where("id = ? AND user_id = ? AND deleted_at IS NULL", id, userID).First(&job)

// Wrap GORM errors — never return gorm.ErrRecordNotFound to callers above repository
if errors.Is(result.Error, gorm.ErrRecordNotFound) {
    return nil, ErrNotFound
}

// TableName override when pluralization is wrong
func (VideoGenerationJob) TableName() string { return "video_generation_jobs" }
```

---

## No Global State

```go
// WRONG: package-level DB variable
var db *gorm.DB

// CORRECT: inject via constructor
type repo struct { db *gorm.DB }
func NewRepo(db *gorm.DB) *repo { return &repo{db: db} }
```

---

## Concurrency

```go
// Always give goroutines a way to stop
go func() {
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            doWork()
        }
    }
}()

// Never use t.Fatal inside a goroutine — use channel
errCh := make(chan error, 1)
go func() {
    if err := doWork(); err != nil {
        errCh <- err
    }
}()
```
