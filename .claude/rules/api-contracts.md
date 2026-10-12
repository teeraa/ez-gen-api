# API Contracts — VIOLET STUDIO Backend

Rules for implementing and maintaining API endpoints. These rules ensure the
frontend and backend stay in sync and clients receive consistent, predictable responses.

---

## Authoritative Source

The backend-authoritative API contract is:
```
docs/engineering/video-generation-api-contract.md
```

The frontend contract is:
```
../ez-gen-web/docs/engineering/api-contract.md
```

**When you change an endpoint, update both documents.** The backend version is authoritative
for field names, HTTP status codes, and error code strings.

---

## Response Envelope

Every response, success or error, uses this envelope:

### Success
```json
{
  "data": { ... },
  "requestId": "550e8400-e29b-41d4-a716-446655440000"
}
```

### List Success
```json
{
  "data": {
    "items": [...],
    "total": 42,
    "page": 1,
    "limit": 20
  },
  "requestId": "..."
}
```

### Error
```json
{
  "code": "VALIDATION_ERROR",
  "message": "Human-readable description",
  "details": [
    { "field": "prompt", "message": "must be at most 2000 characters" }
  ],
  "requestId": "..."
}
```

`details` is only present for `VALIDATION_ERROR`. All other errors omit `details`.

### Forbidden Fields in Error Responses

Never include in error responses:
- Stack traces
- SQL error messages
- BytePlus task IDs
- Provider error payloads
- Internal file paths
- `ARK_API_KEY` or any credential

---

## HTTP Status Codes

| Status | Use case |
|---|---|
| 200 | Successful GET, successful cancel, idempotent POST returning existing resource |
| 201 | Successful POST creating a new resource |
| 204 | Successful DELETE (no body) |
| 400 | Invalid request body or validation failure |
| 401 | Missing or invalid authentication token |
| 402 | Insufficient credits |
| 404 | Resource not found OR resource belongs to another user |
| 409 | Conflict (e.g., job not in a cancellable state) |
| 413 | Uploaded file too large |
| 415 | Unsupported media type |
| 422 | Semantically invalid request (valid fields, but impossible combination) |
| 429 | Rate limited |
| 500 | Unexpected server error |
| 502 | Provider (BytePlus) returned an error |

**Never return 403.** Return 404 for resources that exist but belong to another user.
This prevents user enumeration.

---

## Error Code Strings

Machine-readable codes used in the `code` field:

| Code | HTTP | Meaning |
|---|---|---|
| `UNAUTHORIZED` | 401 | No valid auth token |
| `VALIDATION_ERROR` | 400 | Field validation failed |
| `INSUFFICIENT_CREDITS` | 402 | Not enough credits |
| `JOB_NOT_FOUND` | 404 | Job not found or belongs to another user |
| `PROJECT_NOT_FOUND` | 404 | Project not found or belongs to another user |
| `JOB_NOT_CANCELLABLE` | 409 | Job already in terminal state |
| `JOB_NOT_SHAREABLE` | 409 | Job has no finished video in storage to share or download |
| `IMAGE_REQUIRED` | 422 | Mode requires referenceImageId |
| `IMAGE_EXPIRED` | 422 | Referenced upload has expired |
| `MODEL_NOT_SUPPORTED` | 422 | Model doesn't support requested mode |
| `INVALID_FILE_TYPE` | 400 | Not PNG/JPG/WEBP |
| `FILE_TOO_LARGE` | 413 | Exceeds 8 MB |
| `UNSUPPORTED_MEDIA_TYPE` | 415 | Content-Type mismatch |
| `INVALID_IMAGE` | 422 | File decodes as invalid image |
| `RATE_LIMITED` | 429 | Too many requests or concurrent jobs |
| `PROVIDER_ERROR` | 502 | BytePlus returned an unexpected error |
| `INTERNAL_ERROR` | 500 | Unexpected server error |

Do not invent new error codes without updating both contract documents.

---

## Field Naming

All JSON fields use **camelCase** (not snake_case):

```go
// CORRECT
type JobResponse struct {
    JobID        string `json:"jobId"`
    AspectRatio  string `json:"aspectRatio"`
    CreatedAt    string `json:"createdAt"`
    CompletedAt  *string `json:"completedAt"`  // nullable = pointer
}

// WRONG
type JobResponse struct {
    JobID        string `json:"job_id"`    // snake_case
    CreatedAt    string `json:"created_at"` // snake_case
}
```

---

## Timestamps

All timestamps are ISO 8601 UTC strings:

```go
// In DTO mapping:
CreatedAt: job.CreatedAt.UTC().Format(time.RFC3339),

// Nullable timestamp:
var completedAt *string
if job.CompletedAt != nil {
    s := job.CompletedAt.UTC().Format(time.RFC3339)
    completedAt = &s
}
```

Never return `time.Time` directly in a DTO (it serializes as RFC3339 by default, but
the explicit format makes intent clear and prevents silent timezone issues).

---

## Nullable Fields

Nullable fields must be present in the response as `null`, not omitted:

```go
// CORRECT: null when not set
type JobResponse struct {
    Result      *ResultDTO `json:"result"`      // null or object
    CompletedAt *string    `json:"completedAt"` // null or string
    Error       *ErrorDTO  `json:"error"`       // null or object
}

// WRONG: field disappears when nil
type JobResponse struct {
    Result *ResultDTO `json:"result,omitempty"` // omitempty hides null from frontend
}
```

Exception: `omitempty` is acceptable only when the frontend explicitly expects the
field to be absent (documented in the contract).

---

## Pagination

Standard pagination shape for list endpoints:

```go
type ListResponse[T any] struct {
    Items []T `json:"items"`
    Total int `json:"total"`
    Page  int `json:"page"`
    Limit int `json:"limit"`
}
```

Query param defaults:
- `page` default: 1
- `limit` default: 20, maximum: 50

Enforce the maximum in request validation:
```go
Limit int `form:"limit" validate:"min=1,max=50"`
```

---

## requestId

Every response includes `requestId`. This is a UUID v4 generated per-request by
middleware and stored in `c.Locals("requestID")`.

The helper functions must always include it:
```go
func Success(data any, requestID string) fiber.Map {
    return fiber.Map{"data": data, "requestId": requestID}
}
func Error(code, message string, details any, requestID string) fiber.Map {
    m := fiber.Map{"code": code, "message": message, "requestId": requestID}
    if details != nil { m["details"] = details }
    return m
}
```

---

## Route Group

Video generation endpoints mount on `/open/v1/`:
- `POST   /open/v1/jobs`
- `GET    /open/v1/jobs/:jobId`
- `DELETE /open/v1/jobs/:jobId`
- `POST   /open/v1/uploads`
- `GET    /open/v1/projects`
- `GET    /open/v1/projects/:projectId`
- `DELETE /open/v1/projects/:projectId`
- `GET    /open/v1/credits`

- `POST   /open/v1/jobs/:jobId/shares`
- `GET    /open/v1/jobs/:jobId/download`
- `DELETE /open/v1/shares/:shareId`

All routes in this group require JWT auth middleware.

The unauthenticated `/public/v1/` group (rate limited per client IP) serves only data
reachable through an unguessable share token:
- `GET    /public/v1/shares/:token`
