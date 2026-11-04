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
    "status":          "queued | submitted | processing | succeeded | failed | cancelled",
    "progress":        45,
    "currentStep":     "queued | generating | saving | done | failed | cancelled",
    "estimatedSecondsRemaining": 20,
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
- `progress` (0–100) is an estimate: BytePlus reports no percentage. While `generating` it
  rises linearly to 85 at the expected finish time, then creeps toward 90; `saving` is 95
  and only `succeeded` is 100. The expected time is the average of the last 20 succeeded
  jobs with the same model, duration and quality (falling back to any model, then to
  `30 + 6 × duration` seconds with fewer than 3 samples).
- `estimatedSecondsRemaining` is `null` while `saving` and once the job is terminal; it is 0
  when the job is running past its expected time.
- Clients should never move a progress bar backwards (`max(shown, progress)`).
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

### POST /open/v1/jobs/:jobId/shares — Create Share Link

Returns the job's public share link, creating it on first call. One active link per job;
calling again returns the same link.

**Success — 201 (new) / 200 (existing):**
```json
{
  "data": {
    "shareId":   "uuid",
    "jobId":     "uuid",
    "url":       "https://violetstudio.app/s/<43-char token>",
    "viewCount": 0,
    "createdAt": "2026-10-12T09:00:00Z"
  },
  "requestId": "..."
}
```

| HTTP | code | Cause |
|---|---|---|
| 404 | `NOT_FOUND` | Job does not exist or belongs to another user |
| 409 | `JOB_NOT_SHAREABLE` | Job is not `succeeded`, or its video is not in storage yet |

`url` points at the **frontend** route `/s/:token` (base from `PUBLIC_WEB_BASE_URL`). The
token is the only credential for the public page: 256-bit random, base64url, 43 chars.

---

### DELETE /open/v1/shares/:shareId — Revoke Share Link

**Success — 204** (no body). The public link stops working (allow up to 60 s for caches).
Creating a share for the same job afterwards mints a **new** URL.
**404 `NOT_FOUND`** if the share does not exist, is already revoked, or belongs to another user.

---

### GET /open/v1/jobs/:jobId/download — Download URL (owner)

```json
{
  "data": {
    "url":       "https://<ref>.supabase.co/storage/v1/object/sign/...&download=violet-studio-1a2b3c4d.mp4",
    "filename":  "violet-studio-1a2b3c4d.mp4",
    "expiresAt": "2026-10-12T10:00:00Z"
  },
  "requestId": "..."
}
```

Opening `url` makes the browser save the file. Same 404/409 rules as creating a share.

---

### GET /public/v1/shares/:token — Public Share (no auth)

Used by the frontend share page (server-side) to render the player and Open Graph tags.
Not authenticated; rate limited per client IP (`PUBLIC_RATE_LIMIT_PER_MINUTE`, default 120)
→ `429 RATE_LIMITED`. Response is cacheable: `Cache-Control: public, max-age=60`.

```json
{
  "data": {
    "mediaUrl":     "https://<ref>.supabase.co/storage/v1/object/sign/...",
    "thumbnailUrl": "https://<ref>.supabase.co/storage/v1/object/sign/...",
    "downloadUrl":  "https://<ref>.supabase.co/storage/v1/object/sign/...&download=...",
    "mediaType":    "video",
    "aspectRatio":  "9:16",
    "duration":     5,
    "expiresAt":    "2026-10-12T10:00:00Z",
    "createdAt":    "2026-10-12T09:00:00Z"
  },
  "requestId": "..."
}
```

- `thumbnailUrl` may be `null`.
- The response deliberately omits the prompt, user, job ID and any provider identifiers.
- **404 `NOT_FOUND`** for unknown, malformed or revoked tokens and for deleted jobs. Each
  successful request increments the share's `viewCount`.

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

## Phase-one Character Studio extension (2026-10-15)

See [character-studio-api-contract.md](character-studio-api-contract.md) for the
additive authentication, image generation and personal asset library contract.
Image generation uses its dedicated endpoint; existing video `image_image` mode
is a video reference input mode and must not be presented as image generation.

## Project production extension (2026-10-15)

The implemented project workspace, persisted editor, scene versions/library references,
configured video capabilities and snapshot-safe export additions are specified in
[video-production-api-contract.md](video-production-api-contract.md).
That additive contract governs those new routes and fields; legacy DNA/job routes remain.
Exports now accept 1–20 clips, with immutable source snapshots, optional idempotency,
and explicit retry. Video percentages/ETA are indeterminate until completion.

## Accepted video submission persistence

After a provider returns a task ID, the API attempts to persist it up to three times
within an independent five-second deadline, even if the caller cancels. It never
repeats the provider POST. If persistence remains unavailable, creation returns the
existing application job ID with `status: "queued"`; replay the same idempotency
key rather than starting another paid request. The poller marks queued video jobs
without a provider task ID after two minutes as failed with job error code
`SUBMISSION_INTERRUPTED`. This means acceptance/billing is uncertain: do not blindly
resubmit; inspect provider history or contact an administrator. There is no automatic
provider reconciliation. A process crash before task-ID durability remains a manual
recovery case; these retries reduce the gap without eliminating it.

## Reference images and scene backgrounds

For new video jobs, image assets keep their submitted order and roles. Prompt
labels `Image 1`, `Image 2`, etc. count image items in that same order; text and
video items do not consume image numbers. A `reference_image` uses subject
appearance without borrowing its source backdrop. The existing scene description
and explicit scene setting override determine the environment. If neither names
an environment, no specific replacement environment is prescribed; the server
does not invent a white/studio background.

`first_frame` and `last_frame` retain the complete supplied frame, composition and
background as the requested video start/end. They do not receive subject-only
background replacement instructions. Existing scenes keep their saved mode;
background replacement intent requires choosing reference-image mode rather than
silently changing first-frame semantics. These are prompt instructions, not
actual image segmentation or a guarantee of generated background separation.

Scene camera/setting overrides are sent even without a style profile, and scene
camera overrides retain precedence over style camera settings. Negative DNA tags
are included in the actual text content under `Constraints:`; no unsupported
provider negative-prompt field is added. Role/background controls, scene overrides
and negative constraints precede the creative brief within the existing
2,000-rune prompt budget. Normal-sized controls survive long briefs; control text
that alone exceeds the budget can still be truncated. Character DNA and
consistency notes remain part of the creative input under existing limits.

Only newly compiled submissions use these instructions. Replaying an existing
job with the same request and idempotency key returns that stored job without
changing its original compiled prompt/snapshot or submitting another provider POST.
No face-identity guarantee, model-policy bypass or new request field is introduced.

### Character reference image generation (2026-10-22)

The authoritative character endpoint extension is documented in
[character-studio-api-contract.md](character-studio-api-contract.md#character-reference-image-to-image-2026-10-22).
Image capabilities expose `supportsCharacterReferenceImage`, `characterReferenceMode:
"image_to_image"`, and nullable `characterReferenceUnavailableReason`.
Character reference readiness requires an image model, without a vision analyzer.
New character jobs use compiler `character-design-v2`: supplied `referenceImageId`
is sent directly to Seedream with a synthetic-character prompt preserving visible
structure except explicit edits. Without a reference, generation remains text-only.
Missing image model returns HTTP 503 `IMAGE_UNAVAILABLE`.
Queued descriptive-reference jobs and idempotent replays preserve frozen legacy
behavior. Product behavior and POST request shape are unchanged. Existing Seedance
first-frame video input remains available; image-conditioned prompts
add natural motion, coherent anatomy, physical lighting/material response and temporal
stability while retaining referenced structure and selected visual style. Text-only
video prompts are unchanged. There is no live-action override or acceptance guarantee.

## One-click photo → synthetic character → realistic video

See [studio-pipeline-api-contract.md](studio-pipeline-api-contract.md) for durable server-owned orchestration, exact spoken dialogue, owned Library assets, capabilities and safe retry rules. Requires separately applied migration 019; no browser child-job orchestration.

Studio pipeline character sources: `referenceImageId` upload, mutually exclusive `characterAssetId` owned generated Library character, or neither for text-to-image. `characterPrompt` optional only for Library reuse; scene always required. Responses add `sourceMode: upload|text|library`; capabilities add `textEnabled`, `libraryEnabled`, `libraryAspectRatios`, `libraryDurations`. Library skips image generation. See the authoritative [pipeline contract](studio-pipeline-api-contract.md) for validation, replay and compatibility.

Pipeline validation feedback adds safe `details: [{field,reason,message}]` on facade HTTP400 `VALIDATION_ERROR`. Reasons and combined scene/dialogue prompt-budget semantics are defined in [studio-pipeline-api-contract.md](studio-pipeline-api-contract.md#actionable-pipeline-validation). Existing request hashes, provider budgets and source selection are unchanged.

## Story Flow (isolated additive domain)

See [Story Flow API](story-flow-api-contract.md). Requires migration020; no legacy scenes/jobs/pipelines/exports are repurposed.

## Director (editable successor)

The separate `/open/v1/director` domain follows the
[Director contract](director-api-contract.md). It preserves Story Flow projects and
routes. `POST /open/v1/director/projects/:id/video` turns storyboard shots into clips
with the same video models as the video menu; see the contract for billing and
adapter constraints.

## Independent footage video editor

The phase-one footage editor uses `/open/v1/video-editor` and its own projects,
assets and immutable export snapshots. See [video-editor-api-contract.md](video-editor-api-contract.md)
for the authoritative routes, revision/idempotency semantics, upload constraints
and segment identity. Editor capabilities add `textTransforms`; optional text-layer
`scaleX`/`scaleY` preserve independent stage resizing through save and export.
Existing generation/project/storyboard APIs remain unchanged.

## AI editor phase 2

See [video-editor AI contract](video-editor-ai-api-contract.md) for additive local transcription, Gemini analysis, reviewed draft and revision-guarded apply endpoints. Optional `autoCompose` ranks
and plans the recommended concept, composes grounded caption/title layers and an
owned optional low-volume audio bed, and applies them while preserving manual tracks.

### Studio pipeline draft/final extension (2026-11-02)

The authoritative pipeline fields and endpoint are specified in
[Studio pipeline contract](studio-pipeline-api-contract.md#draft-and-final-rendering-2026-11-02).
`draft` now survives pipeline request persistence and child submission; resolved
model capabilities control support. Pipeline DTOs expose durable `isDraft`,
`canCreateFinal`, model/settings metadata, nullable `draftExpiresAt`,
`finalUnavailableReason` and `sourceDraftId`. `POST /studio-pipelines/:id/finalize`
with only `idempotencyKey` creates an idempotent separate final render from the
owned succeeded draft; original draft and character remain intact. A final uses
the stored provider draft at 1080p. Expired sources return 409 `DRAFT_EXPIRED`.


Studio pipeline Library compatibility: `characterAssetId` accepts a canonical
UUID of any version, including deterministic UUIDv5 IDs returned by pipeline
asset creation. Ownership/provenance validation remains server-side. Upload
`referenceImageId` and create/finalize `idempotencyKey` remain UUIDv4-only; see
[Studio pipeline contract](studio-pipeline-api-contract.md#library-asset-uuid-compatibility-2026-11-02).
