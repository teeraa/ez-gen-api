# Backend Architecture — Agent C Deliverable

> Date: 2026-10-12
> Stack: Go 1.26 · Fiber v2 · GORM · PostgreSQL
> Status: **Proposed** — implementation has not begun

---

## 1. Architectural Principle

The backend uses a **layered architecture** with strict inward-only dependencies.
Each layer has one responsibility. No layer imports the layer above it.

```
HTTP Request
     │
     ▼
┌─────────────┐
│   Handler   │  Parse request, bind to Request struct, call Facade, format response
└──────┬──────┘
       │ calls
       ▼
┌─────────────┐
│   Facade    │  Orchestrate business logic: validate, call repos/services, map to DTO
└──────┬──────┘
       │ calls
  ┌────┴────┐
  ▼         ▼
┌──────┐ ┌─────────┐
│ Repo │ │ Service │  Repo: DB access. Service: external providers (BytePlus, storage).
└──────┘ └─────────┘
       │ both use
       ▼
┌─────────────┐
│    Model    │  GORM struct. No business logic. Pure data representation.
└─────────────┘
```

### Dependency Rules

- Handlers import Facades and DTOs.
- Facades import Repositories, Services, and DTOs.
- Repositories import Models.
- Services import nothing from the app layer (only external packages + config).
- Models have no imports from the app layer.

---

## 2. Package Layout

### `apps/models/`

GORM struct definitions. Conventions:
- Embed `gorm.Model` (ID as uint) or define a custom base with UUID primary key.
- `CreatedAt`, `UpdatedAt` via GORM auto-manage.
- `DeletedAt gorm.DeletedAt` for soft-delete where appropriate.
- No methods that encapsulate business rules.

```go
// Example base (to be confirmed by Agent E's schema proposal)
type VideoGenerationJob struct {
    ID         uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
    CreatedAt  time.Time
    UpdatedAt  time.Time
    DeletedAt  gorm.DeletedAt `gorm:"index"`
    // ... domain fields
}
```

### `apps/repositories/`

One sub-package per domain entity. Each repository:
- Accepts a `*gorm.DB` in its constructor.
- Exposes an interface (for testability) and a concrete implementation.
- Returns domain errors, not GORM-specific errors, to the calling layer.

```
apps/repositories/
├── repository_video_generation/
│   ├── interface.go      # VideoGenerationJobRepository interface
│   └── postgres.go       # PostgreSQL implementation
└── repository_open_api/  # (future user / project repositories)
```

### `apps/facades/`

One sub-package per feature domain. Each facade:
- Accepts repository and service dependencies via constructor injection.
- Contains all business rules (validation beyond field-level, state machine transitions).
- Returns DTOs to the handler layer.
- Never returns raw GORM models or provider-specific structs.

```
apps/facades/
├── facade_video_generation/
│   ├── interface.go
│   └── facade.go
└── facade_open_api/
```

### `apps/handlers/`

One sub-package per route group. Each handler:
- Binds `c.BodyParser` or `c.ParamsParser` into a Request struct.
- Calls the facade.
- Maps the result to a standard response envelope.
- Never contains business logic.

```
apps/handlers/
├── handler_video_generation/
│   └── handler.go        # RegisterRoutes(router fiber.Router)
└── handler_open_api/
```

### `apps/services/`

External integrations. Each service package is self-contained:

```
apps/services/
└── modelark/
    ├── client.go         # HTTP client, auth header injection
    ├── requests.go       # Provider-specific request structs
    ├── responses.go      # Provider-specific response structs
    └── mapper.go         # Application domain ↔ provider domain translation
```

The ModelArk service:
- Must be injectable (accepts interface or config, not global state).
- Must support a mock transport for testing.
- Must map provider status values to application `JobStatus` constants.

### `apps/dto/`

Outbound shapes. Structs used only for JSON serialization.
No GORM tags. Never embed models directly.

```
apps/dto/
└── dto_video_generation/
    ├── job_response.go   # Returned by GET /open/v1/jobs/:id
    └── create_response.go
```

### `apps/requests/`

Inbound shapes. Structs used for request binding and validation.
Use `validate:` tags compatible with `go-playground/validator/v10`.

```
apps/requests/
└── request_video_generation/
    ├── create_job.go     # POST /open/v1/jobs body
    └── list_jobs.go      # GET /open/v1/jobs query params
```

### `apps/crons/`

Background workers for:
- Polling ModelArk task status for in-progress jobs.
- Detecting and recovering stale jobs.
- Cleaning up expired uploads.

Each cron is registered in main.go and runs as a goroutine with a configurable interval.
Do not implement crons before the job lifecycle design is confirmed.

### `apps/db/`

Database connection setup only:

```go
func Connect(dsn string) (*gorm.DB, error)
```

Returns a configured `*gorm.DB`. Does not run migrations.

### `pkgs/configs/`

Configuration loader. Reads environment variables once at startup.
Exposes a `Config` struct with typed fields. No business logic.

---

## 3. Response Envelope

All API responses use a consistent envelope. Proposed shape:

```go
// Success
{
    "data":      <object or array>,
    "requestId": "<uuid>"
}

// Error
{
    "code":      "VALIDATION_ERROR",
    "message":   "Human-readable description",
    "details":   [{"field": "prompt", "message": "required"}],  // optional
    "requestId": "<uuid>"
}
```

The `requestId` is generated per-request (UUID v4) and injected by middleware.
It enables log correlation between frontend and backend.

This envelope must match `../ez-gen-web/docs/engineering/api-contract.md`.

---

## 4. Middleware Stack

Proposed order (registered in main.go):

1. **Request ID** — generate UUID, set in context and response header `X-Request-ID`
2. **Logger** — structured request log (method, path, status, latency, requestId)
3. **Recover** — panic recovery → 500 response
4. **CORS** — controlled by `CORS_ALLOWED_ORIGINS`
5. **Rate Limiter** — per-IP and per-user rate limiting
6. **Auth** — JWT validation for protected routes

---

## 5. Authentication Pattern

> Proposed — details to be confirmed by Agent F (Security Specialist).

JWT bearer token in `Authorization: Bearer <token>` header.

Middleware extracts claims and stores `userID` in Fiber context:
```go
c.Locals("userID", claims.Subject)
```

Handlers and facades receive `userID` from context, not from the request body.

Unauthenticated requests to protected routes → 401.
Authenticated requests to another user's resources → 404 (do not confirm existence).

---

## 6. Error Handling

Errors propagate up the call stack. Each layer wraps with context:

```go
// Repository
return nil, fmt.Errorf("find job %s: %w", id, err)

// Facade
return nil, fmt.Errorf("get generation job: %w", err)

// Handler maps to HTTP:
// - record not found → 404
// - validation error → 400
// - unauthorized → 401
// - provider error → 502
// - unknown → 500
```

Sentinel errors:

```go
var ErrNotFound = errors.New("not found")
var ErrUnauthorized = errors.New("unauthorized")
var ErrConflict = errors.New("conflict")
```

The handler inspects `errors.Is(err, ErrNotFound)` etc. to choose the HTTP status.
Provider-specific errors are mapped by `apps/services/modelark/mapper.go`.

---

## 7. Route Registration

Routes are registered per-handler in a `RegisterRoutes` function:

```go
// handler_video_generation/handler.go
func RegisterRoutes(router fiber.Router, h *Handler) {
    router.Post("/jobs", h.CreateJob)
    router.Get("/jobs/:id", h.GetJob)
    router.Delete("/jobs/:id", h.CancelJob)
    router.Post("/uploads", h.UploadImage)
    router.Get("/projects", h.ListProjects)
    router.Get("/projects/:id", h.GetProject)
    router.Delete("/projects/:id", h.DeleteProject)
    router.Get("/credits", h.GetCredits)
}
```

The main Fiber router attaches this to the `/open/v1` group.

---

## 8. Configuration Injection

Constructor injection is preferred over global state:

```go
type Handler struct {
    facade FacadeInterface
}

func NewHandler(facade FacadeInterface) *Handler {
    return &Handler{facade: facade}
}
```

All dependencies are wired in `main.go`:

```
config → db → repos → services → facades → handlers → routes
```

---

## 9. Video-Generation Component Placement

| Component | Package | Notes |
|---|---|---|
| GORM model | `apps/models/video_generation_job.go` | Schema per `video-data-model-proposal.md` |
| Repository interface + impl | `apps/repositories/repository_video_generation/` | Postgres implementation |
| ModelArk HTTP client | `apps/services/modelark/` | Self-contained; mockable |
| Facade (business logic) | `apps/facades/facade_video_generation/` | Orchestrates repo + service |
| Handler (HTTP binding) | `apps/handlers/handler_video_generation/` | Calls facade only |
| Request structs | `apps/requests/request_video_generation/` | Field validation tags |
| Response DTOs | `apps/dto/dto_video_generation/` | Clean JSON shapes |
| Status poller | `apps/crons/video_generation_poller.go` | Goroutine, configurable interval |
| Route registration | `routes/open_api/v1/video_generation.go` | Mounts on `/open/v1` |

---

## 10. Decisions and Trade-offs

| Decision | Choice | Rationale |
|---|---|---|
| HTTP framework | Fiber v2 | Performance, existing convention in project |
| ORM | GORM v1.25 | Established Go ORM, good PostgreSQL support |
| Migration tool | SQL file migrations | Explicit, auditable, avoids AutoMigrate in production |
| UUID vs uint primary key | UUID | Prevents enumeration attacks on job IDs |
| Interface per repository | Yes | Enables unit testing with mock repos |
| Global vs injected config | Injected via constructor | Testability |
| Multi-provider interface now | No | Only one provider exists; add abstraction when second provider arrives |
