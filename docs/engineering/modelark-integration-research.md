# BytePlus ModelArk — Video Generation API Research

> Researched: 2026-10-12
> Status: DRAFT — verify against live docs before implementation
> Agent: D — ModelArk Integration Specialist
> Sources: github.com/volcengine/ark-runtime-go v0.11.0 (official SDK, Apache-2.0),
>          community integrations (axy-full/aimighty-workspace, jeboong/byteplus-seedance-2-studio),
>          official docs.byteplus.com (SPA — could not extract raw spec programmatically)

---

## Summary for Implementation Agents

| Key Fact | Value |
|---|---|
| Official Go SDK | `github.com/volcengine/ark-runtime-go` (use this — do not write a raw HTTP client) |
| BytePlus base URL | `https://ark.ap-southeast.bytepluses.com/api/v3` |
| Auth header | `Authorization: Bearer <ARK_API_KEY>` |
| Create task | `POST /api/v3/contents/generations/tasks` |
| Get task | `GET /api/v3/contents/generations/tasks/{id}` |
| Cancel task | `DELETE /api/v3/contents/generations/tasks/{id}` |
| Task ID field | `id` (prefix: `cgt-...`) |
| Terminal status values | `succeeded`, `failed`, `cancelled` |
| Polling status values | `queued`, `running` |
| Output video URL field | `response.content.video_url` |
| Output URL lifetime | ~24 hours (must copy to durable storage) |
| Environment variable | `ARK_API_KEY` (matches SDK expectation) |

---

## 1. Authentication

### API Key (Standard)

All API calls use Bearer token authentication:

```
Authorization: Bearer <ARK_API_KEY>
Content-Type: application/json
```

Load the key from environment variable `ARK_API_KEY`. The official SDK reads this
variable by convention:

```go
client := arkruntime.NewByteplusClientWithApiKey(os.Getenv("ARK_API_KEY"))
```

> **Note on .env.example:** The current `.env.example` uses `MODELARK_API_KEY`.
> Update it to `ARK_API_KEY` to match the official SDK convention, OR set
> `ARK_API_KEY = os.Getenv("MODELARK_API_KEY")` in the config loader. A single
> env var name must be decided during implementation.

### AK/SK Alternative (Not recommended for this use case)

The SDK also supports HMAC-based `BYTEPLUS_ACCESSKEY` + `BYTEPLUS_SECRETKEY`
authentication, intended for the control-plane (Asset Library). Use Bearer token
for the generation data-plane.

### Region and Base URL

ModelArk is regionally partitioned. For BytePlus (international):

```
https://ark.ap-southeast.bytepluses.com/api/v3
```

For Volcengine (China region):
```
https://ark.cn-beijing.volces.com/api/v3
```

**Critical:** The API key, the activated model, and the endpoint region must all
agree. Using an incorrect host returns 404. Configure via `ARK_BASE_URL`
environment variable (SDK reads this).

---

## 2. Official Go SDK

**Use the official SDK. Do not write a raw HTTP client from scratch.**

```bash
go get github.com/volcengine/ark-runtime-go@latest
# Latest confirmed version: v0.11.0 (2026-10-11)
```

Packages:
- `github.com/volcengine/ark-runtime-go` — client initialization
- `github.com/volcengine/ark-runtime-go/arkruntime/model/contentgeneration` — request/response types

Client initialization:

```go
client := arkruntime.NewByteplusClientWithApiKey(os.Getenv("ARK_API_KEY"))
// or with custom base URL:
client := arkruntime.NewByteplusClientWithApiKey(
    os.Getenv("ARK_API_KEY"),
    option.WithBaseUrl(os.Getenv("ARK_BASE_URL")),
)
```

Key methods:

| Method | Description |
|---|---|
| `CreateContentGenerationTask(ctx, req)` | Submit a new generation task |
| `GetContentGenerationTask(ctx, taskID)` | Poll task status |
| `ListContentGenerationTasks(ctx, query)` | List tasks |
| `DeleteContentGenerationTask(ctx, taskID)` | Cancel a task |

The SDK handles authentication, serialization, and connection pooling.
Wrap the SDK client inside `apps/services/modelark/client.go` — do not call
it directly from facades.

---

## 3. Video Generation Task Creation

### Endpoint

```
POST https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks
```

### Request Schema

Fields relevant to VIOLET STUDIO (from `oas_schemas_gen.go` in official SDK):

| Field | Type | Required | Notes |
|---|---|---|---|
| `model` | string | **Yes** | See Section 5 for IDs |
| `content` | []ContentItem | **Yes** | Text first, images second |
| `ratio` | string | No | `"16:9"`, `"9:16"`, `"1:1"`, `"4:3"`, `"21:9"` |
| `duration` | int64 | No | Seconds (model-specific range) |
| `resolution` | string | No | `"480p"`, `"720p"`, `"1080p"`, `"4k"` |
| `optimize_prompt` | bool | No | Auto-enhances the prompt; revised prompt in response |
| `generate_audio` | bool | No | Seedance 2.5 only |
| `watermark` | bool | No | Adds BytePlus watermark |
| `seed` | int64 | No | For reproducibility |
| `file_format` | string | No | `"mp4"` (default) or `"mov"` |
| `execution_expires_after` | int64 | No | Seconds until task expires |
| `callback_url` | string | No | Webhook URL for completion notification |
| `return_last_frame` | bool | No | Return last frame as image |

### ContentItem Structure

```go
type ContentItem struct {
    Type     string    // "text" | "image_url" | "video_url" | "audio_url" | "draft_task"
    Text     string    // when Type == "text"
    ImageURL *URLRef   // when Type == "image_url"
    VideoURL *URLRef   // when Type == "video_url"
    AudioURL *URLRef   // when Type == "audio_url"
    DraftTask *DraftTaskRef // when Type == "draft_task"
    Role     string    // "first_frame" | "last_frame" | "reference_image" | "reference_video"
}
```

**Ordering rule:** text element must come first in the array, then image references.

**Image-to-Video:** Set `Role: "first_frame"` on an image ContentItem. The model
interprets this as an image-to-video request. A `last_frame` without a `first_frame`
is rejected.

### Example: Text-to-Video

```json
{
  "model": "dreamina-seedance-2-0-fast-260128",
  "content": [
    { "type": "text", "text": "A knight standing on a rolling plain at dusk" }
  ],
  "ratio": "16:9",
  "duration": 5,
  "resolution": "720p",
  "optimize_prompt": false,
  "watermark": false,
  "execution_expires_after": 3600
}
```

### Example: Image-to-Video

```json
{
  "model": "dreamina-seedance-2-5-260628",
  "content": [
    { "type": "text", "text": "Camera slowly zooms out" },
    { "type": "image_url", "image_url": { "url": "https://..." }, "role": "first_frame" }
  ],
  "ratio": "16:9",
  "duration": 8,
  "resolution": "1080p",
  "generate_audio": true,
  "watermark": false
}
```

### Create Task Response

```json
{ "id": "cgt-XXXXXXXXXXXXXXXXXX" }
```

- **Task ID field:** `id`
- **Prefix:** `cgt-...`
- This ID is stored in `video_generation_jobs.provider_task_id` (internal, never exposed to frontend)

---

## 4. Task Status Retrieval

### Endpoint

```
GET https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks/{taskID}
```

Task ID is a **path parameter** (not a query parameter).

### Response Schema (Key Fields)

```json
{
  "id": "cgt-...",
  "model": "dreamina-seedance-2-0-fast-260128",
  "status": "queued | running | succeeded | failed | cancelled",
  "created_at": 1728700000,
  "updated_at": 1728700120,
  "seed": 12345,
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 5,
  "revised_prompt": "...",
  "content": {
    "video_url": "https://...",
    "last_frame_url": "https://...",
    "file_url": null
  },
  "usage": {
    "completion_tokens": 1000,
    "total_tokens": 1000
  },
  "error": null
}
```

### Status Values (Official)

| Provider Status | Application Status Mapping |
|---|---|
| `queued` | `submitted` (task exists at provider, not yet running) |
| `running` | `processing` |
| `succeeded` | `succeeded` |
| `failed` | `failed` |
| `cancelled` | `cancelled` |

> Application starts at `queued` (before provider task is created) and `submitted`
> (after provider task created, before provider starts running).
> See `video-data-model-proposal.md` for the full state machine.

### Output Video Location

```
response.content.video_url   ← primary output
response.content.last_frame_url  ← thumbnail (if return_last_frame was set)
```

Both fields are `null` when status is not `succeeded`.

---

## 5. Output Media

### URL Type

The output `video_url` is a **pre-signed temporary URL** hosted on BytePlus object storage.

### Expiry

Approximately **24 hours** after generation. (Community confirmed; exact official TTL
not available in accessible documentation. Verify before implementation.)

### Required Workflow (Non-Negotiable)

```
1. Task status == "succeeded"
2. GET response.content.video_url
3. Download the video binary
4. Upload to application object storage (S3/TOS/R2)
5. Store the object storage KEY in video_generation_jobs.output_media_key
6. NEVER store the BytePlus URL in the database
7. Generate signed URLs from app storage on demand when serving to clients
```

Failure to follow this workflow will result in broken media links within 24 hours.

---

## 6. Supported Models (BytePlus API Identifiers)

| Internal ID | BytePlus API Model ID | Max Resolution | Duration Range | Audio | Image-to-Video |
|---|---|---|---|---|---|
| `violet-motion-2` | `dreamina-seedance-2-0-fast-260128` | 720p | 4–15 s | No | Yes |
| `violet-cinema` | `dreamina-seedance-2-0-260128` | 4K | 4–15 s | No | Yes |
| `violet-fast` | (same as violet-motion-2) | 720p | 4–15 s | No | Yes |
| `violet-realism` | `dreamina-seedance-2-5-260628` | 1080p | 4–30 s | Yes | Yes |
| `violet-classic` | `seedance-1-0-pro-250528` | 1080p | 2–12 s (API min 4) | No | Yes (first frame only; no reference images) |

> **Current routing (2026-10-12):** only `seedance-1-0-pro-250528` is activated on the
> ModelArk account, so every internal ID above currently routes to it with Seedance 1.0 Pro
> capabilities (1080p, 2–12 s, no audio, first-frame only). Restore the 2.x mapping in
> `apps/services/modelark/mapper.go` once those models are activated.

> **[IMPORTANT — verify this mapping]** The table above is a **proposed mapping** 
> from VIOLET STUDIO's UI model names to BytePlus model IDs, based on the available 
> models. The actual mapping must be confirmed by the team before implementation.
> In particular, whether `violet-fast` maps to the same model as `violet-motion-2`
> needs a product decision.

### Additional Models Available

| BytePlus Model ID | Notes |
|---|---|
| `dreamina-seedance-2-0-mini-260615` | Lighter variant; capability details unclear — verify |
| `dreamina-seedance-2-5-260628` | Latest, most capable; supports audio, 1080p, draft mode |

### Reference Input Limits

| Model | Max Reference Images | Max Reference Videos |
|---|---|---|
| Seedance 2.5 | 30 | 10 |
| Seedance 2.0 / Fast | 9 | 3 |
| Seedance 2.0 Mini | [UNCLEAR — verify] | [UNCLEAR — verify] |

---

## 7. Supported Parameters

### Aspect Ratios (`ratio` field)

| Value | Description |
|---|---|
| `"16:9"` | Landscape widescreen |
| `"9:16"` | Portrait/vertical |
| `"1:1"` | Square |
| `"4:3"` | Standard |
| `"3:4"` | Portrait standard |
| `"21:9"` | Ultra-wide cinematic |
| `"adaptive"` | Model selects best ratio [UNCLEAR — verify] |

The VIOLET STUDIO frontend supports `16:9`, `9:16`, and `1:1`. All three are available.

### Resolution (`resolution` field)

| Value | Models |
|---|---|
| `"480p"` | All models; also draft mode output |
| `"720p"` | All models |
| `"1080p"` | Seedance 2.5, Seedance 2.0 |
| `"4k"` | Seedance 2.0 (use with caution) |

Map `quality: "standard"` → `"720p"`, `quality: "high"` → `"1080p"` for the
VIOLET STUDIO quality field. (Product decision — confirm before implementation.)

### Duration (`duration` field)

- Seedance 2.5: 4–30 seconds
- Seedance 2.0 / Fast / Mini: 4–15 seconds

The VIOLET STUDIO frontend allows `5` and `10` seconds — both are within all model
limits. No issue here.

### Prompt Optimization (`optimize_prompt`)

Boolean. When `true`, the model rewrites the prompt before generation. The revised
prompt is returned in `response.revised_prompt`. Map to the frontend `enhancePrompt`
field.

---

## 8. Cancellation

### Supported: Yes (queued tasks only)

```
DELETE https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks/{taskID}
```

| Task State at Cancel | Result |
|---|---|
| `queued` | Task moves to `cancelled` — not billed |
| `running` | Returns error (~HTTP 409) — task continues running |
| `succeeded` / `failed` | Task record is deleted from provider |

**Implication for implementation:** The application should only attempt provider-level
cancellation when the application status is `queued` or `submitted`. If `processing`,
the cancel request will fail — the application must still update its own status to
`cancelled` and handle the in-flight generation gracefully.

---

## 9. Error Handling

### HTTP-Level Errors

Returned synchronously:

```json
{ "error": { "code": "ErrorCodeString", "message": "Description" } }
```

### Async Errors

Content policy violations and generation failures appear in the task's `error` field
when status is `"failed"`.

```json
{
  "status": "failed",
  "error": {
    "code": "OutputVideoSensitiveContentDetected",
    "message": "..."
  }
}
```

### Known Error Codes

| Code | Meaning |
|---|---|
| `OutputVideoSensitiveContentDetected` | Output blocked by content moderation |
| `InvalidParameter.TaskTypeConstraint` | Invalid parameter combination |

> Comprehensive error code list not available in accessible docs. [UNCLEAR — verify]

### Recommended HTTP Client Timeouts

| Operation | Timeout |
|---|---|
| Task creation (POST) | 120 seconds |
| Task status poll (GET) | 30 seconds |
| Cancellation (DELETE) | 30 seconds |

---

## 10. Rate Limits and Polling

### Official Rate Limits

Not found in publicly accessible documentation. The BytePlus docs are a JavaScript
SPA that returns empty content to non-browser fetchers. Verify from:
- BytePlus console → ModelArk → Usage / Quotas
- BytePlus account team

Community estimates (early access, not authoritative):
- ~2 requests/second
- ~3 concurrent tasks

### Recommended Polling Interval

No official guidance found. Community implementations use 5–10 seconds.
The official SDK example sleeps 60 seconds between create and first poll.

**Proposed for VIOLET STUDIO:** Poll every 5 seconds (configurable via
`MODELARK_POLL_INTERVAL_SECONDS`). Back off to 15 seconds after 60 consecutive
polls with no status change.

---

## 11. Webhook Support

The `callback_url` field in the create task request accepts a webhook URL for
task completion notification. Behavior:
- BytePlus POSTs to the URL when the task reaches a terminal state
- Payload format: [UNCLEAR — verify from official docs]
- Signature verification: [UNCLEAR — verify whether HMAC signature is provided]

**Polling is the safe default** until webhook payload format and signature verification
are confirmed. Webhooks can be added later as an optimization.

---

## 12. Implementation Checklist

Before writing `apps/services/modelark/client.go`:

- [ ] `ARK_API_KEY` env var name confirmed (vs `MODELARK_API_KEY` in current .env.example)
- [ ] `ARK_BASE_URL` set to `https://ark.ap-southeast.bytepluses.com` in `.env.example`
- [ ] `github.com/volcengine/ark-runtime-go` added to `go.mod`
- [ ] Model ID mapping table confirmed with team (see Section 6)
- [ ] Resolution quality mapping confirmed (`standard` → `720p`, `high` → `1080p`)
- [ ] Output URL expiry tolerance confirmed (24h → download immediately on success)
- [ ] Object storage provider chosen and `STORAGE_*` vars configured

---

## Appendix: Full API Surface

| Operation | SDK Method | HTTP |
|---|---|---|
| Create task | `CreateContentGenerationTask` | POST `/api/v3/contents/generations/tasks` |
| Get task | `GetContentGenerationTask` | GET `/api/v3/contents/generations/tasks/{id}` |
| List tasks | `ListContentGenerationTasks` | GET `/api/v3/contents/generations/tasks` |
| Cancel task | `DeleteContentGenerationTask` | DELETE `/api/v3/contents/generations/tasks/{id}` |

---

## Appendix: Environment Variable Summary

| Variable | Source | Notes |
|---|---|---|
| `ARK_API_KEY` | BytePlus console | Matches official SDK convention |
| `ARK_BASE_URL` | Constant | `https://ark.ap-southeast.bytepluses.com` |

> The current `.env.example` uses `MODELARK_API_KEY` and `MODELARK_BASE_URL`.
> Reconcile these names during the implementation setup step. Either rename the
> env vars to match the SDK, or add an adapter in the config loader.
