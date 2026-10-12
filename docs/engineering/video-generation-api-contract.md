# Video Generation API Contract — Backend Authoritative Version

> Date: 2026-10-12
> Status: **Proposed** — not yet implemented
> Reconciled against: `../ez-gen-web/docs/engineering/api-contract.md` (2026-10-12)
> Backend: Go Fiber (`ez-gen-api`)
> Provider: BytePlus ModelArk (server-side only)

This document is the **backend-authoritative** contract. The frontend contract at
`../ez-gen-web/docs/engineering/api-contract.md` is the frontend-authoritative view.
These two must remain in sync. Any discrepancy must be resolved here first, then
reflected in the frontend document.

---

## Design Principles

1. The frontend never calls BytePlus directly.
2. All job identifiers exposed to the frontend are application-level UUIDs, never provider task IDs.
3. API responses are normalized — no BytePlus-specific fields reach the browser.
4. All errors use a consistent envelope with a machine-readable `code`.
5. Every mutable operation is idempotent where the client supplies an idempotency key.
6. All timestamps are ISO 8601 UTC strings (`2026-10-12T09:00:00Z`).
7. Signed media URLs are generated on demand and not stored in the database.

---

## Base URL

| Environment | Value |
|---|---|
| Local | `http://localhost:8080` |
| Production | `https://api.violetstudio.app` (TBD) |

The Next.js server reads `API_URL` from env. The browser never sees this URL directly.

---

## Route Group

All video-generation endpoints mount under `/open/v1/`.

---

## Authentication

All endpoints require:
```
Authorization: Bearer <jwt-token>
```

Unauthenticated → HTTP 401:
```json
{ "code": "UNAUTHORIZED", "message": "Valid authentication required", "requestId": "..." }
```

> Authentication mechanism not yet fully designed. See `backend-security.md`.
> The contract assumes JWT bearer tokens issued by this backend.

---

## Standard Response Envelopes

### Success
```json
{
  "data": { ... },
  "requestId": "550e8400-e29b-41d4-a716-446655440000"
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
  "requestId": "550e8400-e29b-41d4-a716-446655440000"
}
```

The `requestId` is generated per-request by middleware and must be logged by the frontend.

---

## Endpoints

### POST /open/v1/jobs — Create Generation Job

Submit a new AI video generation request.

**Request body:**
```json
{
  "prompt":           "string (1–2000 chars, required)",
  "mode":             "text | image_video | image_image",
  "model":            "violet-motion-2 | violet-cinema | violet-fast | violet-realism | violet-classic",
  "aspectRatio":      "16:9 | 9:16 | 1:1",
  "duration":         5 | 10,
  "quality":          "standard | high",
  "enhancePrompt":    true | false,
  "referenceImageId": "uuid (required for image modes)",
  "idempotencyKey":   "uuid (optional — prevents duplicate submissions on retry)"
}
```

**Success — HTTP 201:**
```json
{
  "data": {
    "jobId":             "uuid",
    "status":            "queued",
    "estimatedCredits":  12,
    "createdAt":         "2026-10-12T09:00:00Z"
  },
  "requestId": "..."
}
```

**Idempotency:** If `idempotencyKey` matches an existing job for this user, return
HTTP 200 with the existing job's current state instead of creating a new one.

**Error codes:**

| HTTP | code | Cause |
|---|---|---|
| 400 | `VALIDATION_ERROR` | Invalid field(s); `details` names each violation |
| 402 | `INSUFFICIENT_CREDITS` | Not enough credits for this generation |
| 422 | `IMAGE_REQUIRED` | Mode requires `referenceImageId` |
| 422 | `IMAGE_EXPIRED` | Referenced upload has expired |
| 422 | `MODEL_NOT_SUPPORTED` | Model does not support the requested mode |
| 429 | `RATE_LIMITED` | Too many concurrent jobs or requests per minute |
| 500 | `INTERNAL_ERROR` | Unexpected server error |

---

### GET /open/v1/jobs/:jobId — Get Job Status

Poll job progress. Poll every 2 seconds while in `queued` or `processing` state.

**Success — HTTP 200:**
```json
{
  "data": {
    "jobId":           "uuid",
    "status":          "queued | processing | succeeded | failed | cancelled",
    "progress":        0,
    "currentStep":     "Initializing...",
    "model":           "violet-motion-2",
    "aspectRatio":     "16:9",
    "duration":        5,
    "quality":         "standard",
    "creditsCharged":  null,
    "result": {
      "thumbnailUrl":  "https://cdn.violetstudio.app/...",
      "mediaUrl":      "https://cdn.violetstudio.app/...",
      "mediaType":     "video",
      "expiresAt":     "2026-10-12T21:00:00Z"
    },
    "error": null,
    "createdAt":       "2026-10-12T09:00:00Z",
    "updatedAt":       "2026-10-12T09:01:30Z",
    "completedAt":     null
  },
  "requestId": "..."
}
```

- `result` is `null` until `status` is `succeeded`.
- `error` is `null` unless `status` is `failed`.
- `mediaUrl` and `thumbnailUrl` are signed URLs generated from object storage, not BytePlus URLs. They are valid for 1 hour; `result.expiresAt` gives the exact expiry, so re-fetch the job to get a fresh URL after that.
- A job whose provider output could not be copied to storage within 24 hours ends as `failed` with `error.code = "OUTPUT_EXPIRED"`.
- `expiresAt` indicates when the signed URL expires. Client should re-fetch before expiry.

**Error codes:**

| HTTP | code | Cause |
|---|---|---|
| 404 | `JOB_NOT_FOUND` | Job does not exist or belongs to another user |

---

### DELETE /open/v1/jobs/:jobId — Cancel Job

Cancel a queued or processing job.

**Success — HTTP 200:**
```json
{
  "data": { "jobId": "uuid", "status": "cancelled" },
  "requestId": "..."
}
```

**Error codes:**

| HTTP | code | Cause |
|---|---|---|
| 404 | `JOB_NOT_FOUND` | Does not exist or belongs to another user |
| 409 | `JOB_NOT_CANCELLABLE` | Already in terminal state (`succeeded`, `failed`, `cancelled`) |
| 502 | `PROVIDER_ERROR` | BytePlus cancellation API failed (if provider cancellation is supported) |

---

### POST /open/v1/uploads — Upload Reference Image

Upload a reference image before submitting a job. The upload is temporary.

**Request:** `Content-Type: multipart/form-data`
```
file: <binary>   (PNG, JPG, or WEBP, max 8 MB)
```

**Validation:** Magic-byte validation in addition to MIME type header.

**Success — HTTP 201:**
```json
{
  "data": {
    "uploadId":    "uuid",
    "filename":    "reference.png",
    "sizeBytes":   204800,
    "mimeType":    "image/png",
    "expiresAt":   "2026-10-13T09:00:00Z"
  },
  "requestId": "..."
}
```

**Error codes:**

| HTTP | code | Cause |
|---|---|---|
| 400 | `INVALID_FILE_TYPE` | Not PNG/JPG/WEBP |
| 413 | `FILE_TOO_LARGE` | Exceeds 8 MB |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | Content-Type header mismatch |
| 422 | `INVALID_IMAGE` | File passes MIME check but fails image decode |

---

### GET /open/v1/projects — List Projects

List the authenticated user's projects.

**Query parameters:**

| Param | Type | Default | Description |
|---|---|---|---|
| `page` | int | 1 | Page number |
| `limit` | int | 20 | Items per page (max 50) |
| `status` | string | all | Filter: `completed`, `processing`, `failed`, `draft` |
| `q` | string | — | Full-text search on title/prompt (max 200 chars) |

**Success — HTTP 200:**
```json
{
  "data": {
    "items": [
      {
        "projectId":    "uuid",
        "title":        "My generation",
        "status":       "completed",
        "thumbnailUrl": "https://...",
        "prompt":       "A sunset over mountains",
        "model":        "violet-motion-2",
        "duration":     5,
        "aspectRatio":  "16:9",
        "createdAt":    "2026-10-12T09:00:00Z",
        "updatedAt":    "2026-10-12T09:01:30Z"
      }
    ],
    "total": 42,
    "page": 1,
    "limit": 20
  },
  "requestId": "..."
}
```

---

### GET /open/v1/projects/:projectId — Get Project

Full project detail including media URL.

**Success — HTTP 200:**
```json
{
  "data": {
    "projectId":         "uuid",
    "title":             "My generation",
    "status":            "completed",
    "jobId":             "uuid",
    "prompt":            "A sunset over mountains",
    "model":             "violet-motion-2",
    "aspectRatio":       "16:9",
    "duration":          5,
    "quality":           "standard",
    "creditsCharged":    12,
    "thumbnailUrl":      "https://...",
    "mediaUrl":          "https://...",
    "mediaType":         "video",
    "mediaExpiresAt":    "2026-10-12T21:00:00Z",
    "referenceImageUrl": null,
    "createdAt":         "2026-10-12T09:00:00Z",
    "updatedAt":         "2026-10-12T09:01:30Z"
  },
  "requestId": "..."
}
```

**Error codes:**

| HTTP | code | Cause |
|---|---|---|
| 404 | `PROJECT_NOT_FOUND` | Does not exist or belongs to another user |

---

### DELETE /open/v1/projects/:projectId — Delete Project

Remove a project and its associated media.

**Success — HTTP 204** (no body)

Media in object storage is deleted asynchronously.

---

### GET /open/v1/credits — Get Credit Balance

```json
{
  "data": {
    "used":      14,
    "total":     100,
    "remaining": 86,
    "resetsAt":  "2026-11-01T00:00:00Z",
    "plan":      "creator"
  },
  "requestId": "..."
}
```

---

## Model Name Mapping

| UI label | API `model` value | BytePlus model ID |
|---|---|---|
| Violet Motion 2.0 | `violet-motion-2` | [see modelark-integration-research.md] |
| Violet Cinema | `violet-cinema` | [see modelark-integration-research.md] |
| Violet Fast | `violet-fast` | [see modelark-integration-research.md] |
| Violet Realism | `violet-realism` | [see modelark-integration-research.md] |
| Violet Classic | `violet-classic` | [see modelark-integration-research.md] |

Each model's aspect-ratio list and duration range are enforced server-side; a request outside them, or `image_image` mode on a model without reference-image support (`violet-classic`), returns `422 MODEL_NOT_SUPPORTED`.

The mapping from `model` → BytePlus model ID lives in `model_configs` table
(or a Go constant map until the database table is populated).

---

## Rate Limiting

| Limit | Value | Response |
|---|---|---|
| Concurrent jobs per user | 3 | HTTP 429 `RATE_LIMITED` |
| Job submissions per minute per user | 10 | HTTP 429 `RATE_LIMITED` |
| Uploads per hour per user | 20 | HTTP 429 `RATE_LIMITED` |

All rate-limit values are configurable via environment variables.

---

## CORS

Allowed origins are controlled by `CORS_ALLOWED_ORIGINS` env var.
For local development: `http://localhost:3000`.

---

## Unresolved Questions (inherited from frontend contract + backend additions)

| Question | Impact | Owner |
|---|---|---|
| Authentication mechanism | Affects all authenticated calls | Agent F |
| BytePlus actual model IDs | Model name mapping incomplete | Agent D |
| Signed URL lifetime | Drives `expiresAt` values | Agent D + E |
| Object storage provider | Affects media URL format | Decision needed |
| Webhook vs polling | Polling assumed; SSE/WebSocket possible | Agent D |
| Exact BytePlus status strings | Drives status mapper | Agent D |
| Project vs Job distinction | Are they the same entity? | Agent C + E |
| CORS exact origins (production) | Affects browser access | Agent F |
