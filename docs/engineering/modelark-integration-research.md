# BytePlus ModelArk — Video Generation API Research

> Researched: 2026-10-12
> Last verified: 2026-11-01
> Status: UPDATED — SDK v0.11.0 source verified 2026-11-01; official docs remain SPA-blocked
> Agent: D — ModelArk Integration Specialist
> Sources: github.com/volcengine/ark-runtime-go v0.11.0 (official SDK, Apache-2.0),
>          SDK installed at /Users/mac/go/pkg/mod/github.com/volcengine/ark-runtime-go@v0.11.0,
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

Fields from `oas_schemas_gen.go` in official SDK v0.11.0 — **SDK-CONFIRMED** unless marked otherwise:

| Field | JSON key | Type | Required | Notes |
|---|---|---|---|---|
| `Model` | `model` | string | **Yes** | See Section 5 for IDs |
| `Content` | `content` | []ContentItem | **Yes** | Text first, images second |
| `Ratio` | `ratio` | string | No | `"16:9"`, `"9:16"`, `"1:1"`, `"4:3"`, `"21:9"` |
| `Duration` | `duration` | int64 | No | Seconds (model-specific range) |
| `Resolution` | `resolution` | string | No | `"480p"`, `"720p"`, `"1080p"`, `"4k"` |
| `OptimizePrompt` | `optimize_prompt` | bool | No | Auto-enhances the prompt; revised prompt in response |
| `GenerateAudio` | `generate_audio` | bool | No | Seedance 2.5 only |
| `Watermark` | `watermark` | bool | No | Adds BytePlus watermark |
| `Seed` | `seed` | int64 | No | For reproducibility |
| `FileFormat` | `file_format` | string | No | `"mp4"` (default) or `"mov"` |
| `ExecutionExpiresAfter` | `execution_expires_after` | int64 | No | Seconds until task expires |
| `CallbackURL` | `callback_url` | string | No | Webhook URL for completion notification |
| `ReturnLastFrame` | `return_last_frame` | bool | No | Return last frame as image |
| **`Draft`** | **`draft`** | **bool** | **No** | **"Mark this task as a draft (cheaper, lower-quality)." SDK-CONFIRMED v0.11.0** |
| `Frames` | `frames` | int64 | No | Alternative to `duration` (frame count) |
| `SubdivisionLevel` | `subdivision_level` | string | No | Render-quality tier (`"high"`, `"medium"`, `"low"`) |
| `CameraFixed` | `camera_fixed` | bool | No | Lock the camera; disables motion |
| `ServiceTier` | `service_tier` | string | No | Service tier hint (`"default"`, `"priority"`) |
| `HumanFaceMode` | `human_face_mode` | string | No | Face handling mode (`"strict"`, `"relaxed"`) |
| `SafetyIdentifier` | `safety_identifier` | string | No | End-user identifier for safety/abuse signals |
| `ReasoningEffort` | `reasoning_effort` | string | No | `"low"`, `"medium"`, `"high"`, `"max"` |
| `Tools` | `tools` | []Tool | No | Tools model may invoke; only `"web_search"` defined |

### ContentItem Structure (SDK-CONFIRMED v0.11.0)

```go
// SDK: arkruntime/model/contentgeneration/oas_schemas_gen.go
type ContentItem struct {
    Type      ContentType  // "text" | "image_url" | "video_url" | "audio_url" | "draft_task"
    Text      OptString    // when Type == "text"
    ImageURL  OptImageURL  // when Type == "image_url"
    VideoURL  OptVideoURL  // when Type == "video_url"
    AudioURL  OptAudioURL  // when Type == "audio_url"
    DraftTask OptDraftTask // when Type == "draft_task" — wraps DraftTask{ID string}
    Role      OptString    // "first_frame" | "last_frame" | "reference_image" | "reference_video"
}

type DraftTask struct {
    ID string `json:"id"` // "Identifier of the draft task to refine."
}
```

ContentType constants (SDK-CONFIRMED):
- `"text"`, `"image_url"`, `"video_url"`, `"audio_url"`, `"draft_task"`

**Ordering rule:** text element must come first in the array, then image references.

**Image-to-Video:** Set `Role: "first_frame"` on an image ContentItem. The model
interprets this as an image-to-video request. A `last_frame` without a `first_frame`
is rejected.

**Draft-to-Final Refinement:** Pass a `ContentItem{Type: "draft_task", DraftTask: {ID: "<draft-task-id>"}}` in the content array of a new task to refine a completed draft into final quality. The response `ContentGenerationTask.DraftTaskID` field echoes the upstream draft ID on refinement tasks. This workflow is SDK-CONFIRMED and official documentation now confirms Seedance 2.5 draft480p → final1080p; see Section 13 (verified 2026-11-02).

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

> **Routing is configured by `ARK_DEFAULT_MODEL`.** When set, every internal ID above is
> sent to that BytePlus model using that model's capabilities (e.g. `seedance-1-0-pro-250528`:
> 1080p, 2–12 s, no audio, first-frame only). When empty, each internal ID uses the mapping
> in the table. Valid values are the keys of `providerModels` in
> `apps/services/modelark/mapper.go`; the server refuses to start on any other value.

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

| Value | Models | Notes |
|---|---|---|
| `"480p"` | All listed models | Required for Seedance 2.5 Draft mode; see Section 13 |
| `"720p"` | All models | Current default for `quality: "standard"` |
| `"1080p"` | Seedance 2.5, Seedance 2.0 | Current default for `quality: "high"` |
| `"4k"` | Seedance 2.0 (use with caution) | Not confirmed for Seedance 2.5 |

Map `quality: "standard"` → `"720p"`, `quality: "high"` → `"1080p"` for the
VIOLET STUDIO quality field. Seedance 2.5 draft requests explicitly use
`draft:true` and `resolution:"480p"`; final refinement explicitly uses
`resolution:"1080p"` and omits inherited parameters. See Section 13 for the
verified request rules. The older advice to omit draft resolution is superseded.

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

## 13. Draft Mode (Seedance 2.5)

Verified 2026-11-02 using official BytePlus documentation and installed SDK v0.11.0.
This section supersedes the 2026-11-01 unresolved draft research below and any
older guidance to omit draft resolution or postpone refinement.

Sources:
- [Dreamina Seedance 2.5 tutorial](https://docs.byteplus.com/en/docs/ModelArk/seedance-2-5)
- [Create video generation task](https://console.byteplus.com/ark/region%3Aap-southeast-1/docs/create-video-generation-task-api)
- [Model list](https://docs.byteplus.com/en/docs/modelark/model-list?redirect=1)

Primary documentation is available through indexed page content; some direct page
opens still return SPA shells or sign-in redirects. No paid API verification ran.

### Verified two-pass contract

Draft creation uses the resolved Seedance 2.5 provider with `draft:true` and
`resolution:"480p"`. Final creation uses the **same provider model**,
`resolution:"1080p"`, and a single `draft_task` content item referencing the
successful draft provider task. Omit draft (or false) for the final request.
The final must omit prompt, input assets, duration, ratio, seed, generate_audio,
and omni_reference_task_type; the provider inherits these from the draft and
rejects resubmission even when values match. Draft and final are billed separately.

Draft task validity runs from provider creation: fourteen days for tasks created
at/after 2026-10-28 10:00 UTC+8 (02:00 UTC), seven days for earlier tasks. The app uses its job
creation time conservatively, which precedes provider submission. Expired drafts
remain playable in durable storage but cannot be refined.

### Application boundaries and verification

`ModelConfig.SupportsDraft` checks the resolved provider model, preserving account
overrides. Public labels likewise reflect the actual resolved Seedance variant.
The pipeline pins the source model; the job facade additionally stores provider
provenance in existing effective_inputs JSON and checks it before refinement.
Legacy drafts lacking provenance are rejected rather than guessing a provider.
The internal-only source application job UUID is owner-scoped, succeeded, draft,
and unexpired; raw provider IDs are never accepted from browser requests.
Final job request hashing includes source identity. Replays return the durable
job before expiry checks, preventing duplicate billed provider submissions.

Final generation does not sign or re-read old character media. It reuses the
source frozen inputs and sends a minimal provider request. Existing SDK optional
fields permit forbidden parameters to be absent. Fake transport tests assert the
serialized payload, and facade tests cover source eligibility, provenance,
expiry, idempotency, draft480p and final1080p without provider calls.


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

---

## Research Log

The 2026-11-01 entry below records historical unresolved findings; Section 13 contains the current official verification.

### 2026-11-01 — Draft Mode verification for Seedance 2.5

**Questions answered:**
1. What is the exact SDK parameter name for draft mode?
   → **CONFIRMED:** `Draft OptBool \`json:"draft"\`` in `CreateContentGenerationTaskRequest`. Comment: "Mark this task as a draft (cheaper, lower-quality)."
2. What content type enables draft-to-final refinement?
   → **CONFIRMED:** `ContentTypeDraftTask = "draft_task"` with `DraftTask{ID string}` — pass in `content` array to reference a previous draft.
3. What is the draft output resolution?
   → **UNRESOLVED** — no SDK constant; `"480p"` noted historically is unconfirmed.
4. Does the SDK confirm which models support `draft: true`?
   → **UNRESOLVED** — SDK schema is model-agnostic. No per-model constraint in the Go struct.
5. Are there new fields in SDK v0.11.0 not yet in `client.go`?
   → **YES (SDK-CONFIRMED):** `CameraFixed`, `SubdivisionLevel`, `Frames`, `HumanFaceMode`, `ServiceTier`, `SafetyIdentifier`, `ReasoningEffort`, `Tools` — all present in SDK but not currently mapped in `apps/services/modelark/client.go`'s `CreateTaskRequest` struct.

**Sources used:**
- `/Users/mac/go/pkg/mod/github.com/volcengine/ark-runtime-go@v0.11.0/arkruntime/model/contentgeneration/oas_schemas_gen.go` (read directly)
- `/Users/mac/go/pkg/mod/github.com/volcengine/ark-runtime-go@v0.11.0/arkruntime/content_generation.go` (read directly)
- `https://raw.githubusercontent.com/volcengine/ark-runtime-go/main/arkruntime/model/contentgeneration/oas_schemas_gen.go` (WebFetch — confirmed matches installed v0.11.0)
- `https://docs.byteplus.com/en/docs/ModelArk/*` (WebFetch — all SPA-blocked, no content extracted)
- `https://github.com/axy-full/aimighty-workspace` (WebFetch — no draft mode docs found)
- `https://github.com/jeboong/byteplus-seedance-2-studio` (WebFetch — no draft mode docs found)

**Remaining blockers:**
- BytePlus official docs remain inaccessible via automated fetch (SPA rendering). Draft mode behavioral details (resolution, cost, model constraints) require either browser access to the docs or live API testing.

## Thai spoken dialogue prompt guidance (2026-11-02)

Verified against the official [Seedance 2.5 tutorial](https://docs.byteplus.com/en/docs/ModelArk/seedance-2-5)
and [create-task API](https://docs.byteplus.com/en/docs/ModelArk/create-video-generation-task-api)
using indexed primary-source content on 2026-11-02 (direct opens can return SPA
shells). The tutorial recommends separating dialogue with braces and providing a
language hint for non-Chinese speech; the API recommends quoting spoken text.
No external prompt skill was installed and no paid audio generation was run.

For resolved `dreamina-seedance-2-5-260628`, the compiler adds a Thai hint only
when meaningful Thai letters outnumber letters from other scripts. Combining
marks, digits, punctuation and Thai repetition/abbreviation signs do not determine
the language. This is a conservative prompt heuristic, not language recognition:
Thai-dominant speech with an English brand receives the hint; an English sentence
containing a Thai brand or an equal mixed utterance retains the generic prompt.
Account overrides follow the resolved provider, not the internal model alias.

Thai formatting uses `{Thai: <JSON-quoted original dialogue>}` and requests clear
native pronunciation with an unhurried conversational pace. Existing native-audio,
natural lip-sync and spoken-versus-subtitle intent remain. It neither translates
nor shortens words and does not mute music or change the submitted sound intent.
Quotes, whitespace, newlines and embedded English names remain exact in the
quoted text and immutable snapshot. Other models and non-Thai-dominant dialogue
keep the existing generic speech template; absent dialogue gains no instruction.

The common compiler counts the complete language prefix, braces, JSON quoting,
scene and reference controls against the existing 2,000-rune budget. Pipeline
preflight invokes this same compiler, so it uses identical counting. Over-budget
requests keep the existing actionable prompt_budget error; dialogue is never
truncated to fit. The existing individual input and quoted-encoding caps stay.
Tests verify model overrides, mixed languages, exact words, serialized SDK content
and exact boundary acceptance/rejection with fake providers. Perceived audio
quality and lip-sync improvement still require listening to a generated sample;
prompt correctness alone does not guarantee either outcome.

## Terminal generation failures versus uncertain submission (2026-11-02)

A task accepted with a provider task ID can later finish with `failed`. This is
an affirmative generation outcome, distinct from an interrupted/unknown create
request. An SDK task error in family `OutputVideoSensitiveContentDetected`
(including the policy-violation suffix observed by authorized read-only diagnosis)
now maps to `PROVIDER_OUTPUT_REJECTED`. The fixed Thai message explains generated
output was rejected by content policy and asks the user to revise the scene or
reference; it does not infer a face, copyright, or other unreported cause.

Unknown or absent error codes on a terminal failed task map to
`PROVIDER_GENERATION_FAILED`, not the submission-uncertainty `PROVIDER_ERROR`.
Submission errors and timeouts preserve their existing semantics. The polling
facade stores a minimal `provider_error` JSON object in the existing JSONB column:
`{"phase":"generation","providerCode":"OutputVideoSensitiveContentDetected"}`.
The code family is allowlisted; unknown families are omitted. Raw suffixes,
messages, prompts, URLs, credentials and request IDs are not retained. HTTP status
and parameter are omitted because task-level SDK errors do not provide them.
Polling HTTP errors do not fabricate a terminal generation result or resubmit.

Fake SDK task tests and fake facade polling tests verify policy and unknown
terminal cases, unchanged submission/timeout handling, bounded diagnostic storage
and zero create/retry calls. No actual failed-job record was changed or retried
for these checks. Existing legacy rows without diagnostics cannot recover an exact
policy reason from the database alone; any projection uses only affirmative
terminal status evidence and must not invent the missing reason.
