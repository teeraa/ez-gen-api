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

Every JSON response, success or error, uses one envelope (built by `apps/utils/response.go`;
always go through `utils.Success` / `utils.Error`, never `c.JSON` directly):

```json
{
  "code": 200,
  "timestamp": "2026-11-17T15:14:57.510357+07:00",
  "transactionCode": "77c1f0b2-5474-42cd-a4a7-217129d87bb4",
  "success": true,
  "titleMessage": "",
  "message": "Success",
  "payload": {}
}
```

| Field | Meaning |
|---|---|
| `code` | HTTP status code (number), same as the response status |
| `timestamp` | Server time, ISO 8601 with microseconds and offset (zone from `APP_TIMEZONE`, default `Asia/Bangkok`) |
| `transactionCode` | Per-request UUID, also sent as the `X-Request-ID` header |
| `success` | `true` for 2xx, `false` otherwise |
| `titleMessage` | Optional UI title; empty unless a handler sets one |
| `message` | `"Success"` on success; human-readable error text on failure |
| `payload` | The data on success (`{}` when there is none); `ErrorPayload` on failure |

### List Success
`payload` is `{ "items": [...], "total": 42, "page": 1, "limit": 20 }`.

### Error
```json
{
  "code": 400,
  "timestamp": "2026-11-17T15:14:57.510357+07:00",
  "transactionCode": "...",
  "success": false,
  "titleMessage": "",
  "message": "Human-readable description",
  "payload": {
    "errorCode": "VALIDATION_ERROR",
    "details": [{ "field": "prompt", "message": "must be at most 2000 characters" }]
  }
}
```

`payload.errorCode` is the machine-readable code from the table below.
`payload.details` is only present for `VALIDATION_ERROR`.

Exceptions with no envelope: `204 No Content`, `304 Not Modified`, and the
`text/event-stream` AI chat reply.

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
| `INSUFFICIENT_POINTS` | 402 | Not enough points for an AI use; `payload.details` = `{ required, balance }` |
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

## transactionCode

`transactionCode` is a UUID v4 generated per request by `middlewares.RequestID()`
and stored in `c.Locals("requestID")`. `utils.Success` / `utils.Error` fill it in.

---

## Route Group

All authenticated endpoints mount on `/api/v1/`:
- `POST   /api/v1/jobs`
- `GET    /api/v1/jobs/:jobId`
- `DELETE /api/v1/jobs/:jobId`
- `POST   /api/v1/uploads`
- `GET    /api/v1/projects`
- `GET    /api/v1/projects/:projectId`
- `DELETE /api/v1/projects/:projectId`
- `GET    /api/v1/credits`

- `POST   /api/v1/jobs/:jobId/shares`
- `GET    /api/v1/jobs/:jobId/download`
- `DELETE /api/v1/shares/:shareId`

All routes in this group require JWT auth middleware.

The unauthenticated `/api/v1/public/` group (rate limited per client IP) serves only data
reachable through an unguessable share token:
- `GET    /api/v1/public/shares/:token`

---

## AI Point Charging

Every AI use is charged from the shared point wallet (`director_point_accounts`).
Director and Film keep their own job ledgers; everything else goes through
`facade_open_api` `chargePoints` / `refundPoints` (ledger: `ai_point_charges`,
`ai_point_entries`, migration 039). Prices come from `AI_*_POINTS` env vars
(`AI_CHAT_MESSAGE_POINTS=3` per AI chat message). Charge before calling the provider,
keyed by a ref that names exactly one AI use (request/idempotency id or a fresh UUID),
and refund when the provider fails or nothing is saved. A failed charge returns
`402 INSUFFICIENT_POINTS` (synchronous) or a job/shot failure code `INSUFFICIENT_POINTS`
(background work). New AI features must charge the same way.
