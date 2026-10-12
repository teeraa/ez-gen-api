# VIOLET DNA — API Contract

> Date: 2026-10-12
> Status: **Implemented**
> Route group: `/open/v1/dna/` (VIOLET DNA) + `/open/v1/jobs/` (extended)
> Auth: All endpoints require `Authorization: Bearer <jwt-token>`

---

## Overview

VIOLET DNA is the character and style consistency system. It provides:

| Resource | Description |
|---|---|
| **Tags** | Reusable creative-direction tokens grouped by category |
| **Character Profiles** | Character identity (appearance, clothing, consistency notes) |
| **Style Profiles** | Visual language (style, lighting, camera, mood) |
| **Scenes** | Generation scenes that reference character + style profiles |
| **Continue Scene** | Creates a new scene linked to a previous one and submits a job |

All endpoints follow the standard response envelope:

```json
// Success
{ "data": { ... }, "requestId": "uuid" }

// Error
{ "code": "ERROR_CODE", "message": "description", "requestId": "uuid" }
// VALIDATION_ERROR also includes:
{ "details": [{ "field": "name", "message": "is required" }] }
```

---

## Tags

### GET /open/v1/dna/tags

List all active tags, optionally filtered by category.

**Query parameters:**

| Param | Type | Required | Description |
|---|---|---|---|
| `category` | string | No | Filter to one category (see categories below) |

**Response — 200:**
```json
{
  "data": [
    {
      "id":          "uuid",
      "slug":        "cinematic",
      "name":        "Cinematic",
      "category":    "visual_style",
      "description": "Film-quality visuals with cinematic framing",
      "sortOrder":   40,
      "createdAt":   "2026-10-12T09:00:00Z"
    }
  ],
  "requestId": "..."
}
```

---

### GET /open/v1/dna/tags/categories

List all supported tag categories.

**Response — 200:**
```json
{
  "data": [
    "character", "visual_style", "environment", "lighting_color",
    "camera", "motion", "mood", "audio", "quality", "negative_constraint"
  ],
  "requestId": "..."
}
```

**Tag categories and their meaning:**

| Category | Use |
|---|---|
| `character` | Character-specific attributes |
| `visual_style` | Overall rendering style (photorealistic, anime, etc.) |
| `environment` | Scene environment and setting |
| `lighting_color` | Lighting and color grading |
| `camera` | Camera movement and framing |
| `motion` | Motion characteristics |
| `mood` | Emotional atmosphere |
| `audio` | Audio generation hints |
| `quality` | Output quality preferences |
| `negative_constraint` | What to avoid (do NOT appear in the compiled prompt text) |

---

## Character Profiles

### POST /open/v1/dna/characters

Create a new character profile.

**Request body:**
```json
{
  "name":             "Elena Voss",
  "description":      "A battle-hardened knight from the northern realm",
  "appearance":       "tall woman with sharp features and silver-grey eyes",
  "hairstyle":        "short platinum blonde",
  "clothing":         "ornate crimson armour with gold trim",
  "accessories":      "worn leather gauntlets, silver pendant",
  "definingFeatures": "a vertical scar across the left cheek",
  "consistencyNotes": "Always shown with confident posture; never shows fear",
  "tagIds":           ["uuid", "uuid"]
}
```

**Validation:**

| Field | Rules |
|---|---|
| `name` | required, 1–100 chars |
| `description` | max 1000 chars |
| `appearance` | max 500 chars |
| `hairstyle` | max 200 chars |
| `clothing` | max 300 chars |
| `accessories` | max 300 chars |
| `definingFeatures` | max 500 chars |
| `consistencyNotes` | max 1000 chars |
| `tagIds` | each must be a valid UUID |

**Response — 201:**
```json
{
  "data": {
    "id":               "uuid",
    "name":             "Elena Voss",
    "description":      "...",
    "appearance":       "...",
    "hairstyle":        "...",
    "clothing":         "...",
    "accessories":      "...",
    "definingFeatures": "...",
    "consistencyNotes": "...",
    "version":          1,
    "tags": [
      { "id": "uuid", "slug": "cinematic", "name": "Cinematic", "category": "visual_style", ... }
    ],
    "createdAt": "2026-10-12T09:00:00Z",
    "updatedAt": "2026-10-12T09:00:00Z"
  },
  "requestId": "..."
}
```

**Important:** `version` increments on every update. Historical generation job
snapshots preserve the version number at the time of submission.

---

### GET /open/v1/dna/characters

List the authenticated user's character profiles.

**Query:** `?page=1&limit=20` (limit max 50)

**Response — 200:**
```json
{
  "data": {
    "items": [ { ...CharacterProfileDTO... } ],
    "total": 12,
    "page":  1,
    "limit": 20
  },
  "requestId": "..."
}
```

---

### GET /open/v1/dna/characters/:id

Get a single character profile.

**Error codes:**

| HTTP | code | Cause |
|---|---|---|
| 404 | `NOT_FOUND` | Does not exist or belongs to another user |

---

### PUT /open/v1/dna/characters/:id

Update a character profile. All fields are optional (partial update).
Each update increments `version`.

**This does NOT modify the effective-inputs snapshot of existing generation jobs.**

**Request body:** same shape as POST, all fields optional (use `*string`).

**Response — 200:** updated CharacterProfileDTO

---

### DELETE /open/v1/dna/characters/:id

Soft-delete a character profile.

**Response — 204** (no body)

---

## Style Profiles

### POST /open/v1/dna/styles

**Request body:**
```json
{
  "name":           "Violet Noir",
  "description":    "Dark neo-noir aesthetic",
  "visualStyle":    "cinematic",
  "colorPalette":   "deep violet and gold",
  "lighting":       "low-key side lighting",
  "mood":           "mysterious",
  "cameraLanguage": "slow dolly push-in",
  "qualityNotes":   "maximum detail, sharp focus on subject",
  "tagIds":         ["uuid"]
}
```

**Validation:**

| Field | Rules |
|---|---|
| `name` | required, 1–100 chars |
| `visualStyle` | max 200 chars |
| `colorPalette` | max 200 chars |
| `lighting` | max 200 chars |
| `mood` | max 200 chars |
| `cameraLanguage` | max 300 chars |
| `qualityNotes` | max 300 chars |

**Response — 201:** StyleProfileDTO

**StyleProfileDTO:**
```json
{
  "id":             "uuid",
  "name":           "Violet Noir",
  "description":    "...",
  "visualStyle":    "cinematic",
  "colorPalette":   "...",
  "lighting":       "...",
  "mood":           "mysterious",
  "cameraLanguage": "slow dolly push-in",
  "qualityNotes":   "...",
  "tags":           [ ...TagDTO... ],
  "createdAt":      "2026-10-12T09:00:00Z",
  "updatedAt":      "2026-10-12T09:00:00Z"
}
```

### GET /open/v1/dna/styles         — list (paginated)
### GET /open/v1/dna/styles/:id     — get single
### PUT /open/v1/dna/styles/:id     — update (partial)
### DELETE /open/v1/dna/styles/:id  — soft-delete → 204

---

## Scenes

### POST /open/v1/dna/scenes

Create a scene that references character and style profiles.

**Request body:**
```json
{
  "title":              "The Gate Assault",
  "description":        "Elena charges toward the castle gate at dawn",
  "characterProfileId": "uuid",
  "styleProfileId":     "uuid",
  "previousSceneId":    "uuid",
  "sceneOrder":         2,
  "actionOverride":     "Elena charges forward, sword raised",
  "cameraOverride":     "low-angle tracking shot",
  "settingOverride":    "castle gate at dawn, cobblestones, fog",
  "tagIds":             ["uuid"]
}
```

**Precedence rules for the Prompt Compiler:**

| Priority | Source | What it controls |
|---|---|---|
| 1 (highest) | `actionOverride` | replaces scene `description` as the primary action |
| 2 | `cameraOverride` | replaces `styleProfile.cameraLanguage` |
| 3 | `settingOverride` | appended after style section |
| 4 | `styleProfile.cameraLanguage` | used when no cameraOverride |
| 5 (lowest) | `styleProfile.*` | base visual language |

**Response — 201:** SceneDTO

**SceneDTO:**
```json
{
  "id":                 "uuid",
  "title":              "The Gate Assault",
  "description":        "...",
  "characterProfileId": "uuid",
  "styleProfileId":     "uuid",
  "previousSceneId":    "uuid",
  "selectedJobId":      null,
  "sceneOrder":         2,
  "tags":               [ ...TagDTO... ],
  "createdAt":          "2026-10-12T09:00:00Z",
  "updatedAt":          "2026-10-12T09:00:00Z"
}
```

### GET /open/v1/dna/scenes         — list ordered by sceneOrder ASC
### GET /open/v1/dna/scenes/:id     — get with preloaded profiles + tags
### PUT /open/v1/dna/scenes/:id     — update (partial)
### DELETE /open/v1/dna/scenes/:id  — soft-delete → 204

---

### PUT /open/v1/dna/scenes/:id/select-output/:jobId

Mark a generation job as the selected output for this scene.

**Response — 200:**
```json
{
  "data": { "sceneId": "uuid", "selectedJobId": "uuid" },
  "requestId": "..."
}
```

---

## Continue Scene

### POST /open/v1/dna/scenes/:id/continue

Create a new scene as a sequel to the specified scene, then immediately
submit a generation job for it.

The new scene **inherits** from the previous scene:
- `characterProfileId` (from previous scene)
- `styleProfileId` (from previous scene)
- all tags (copied)

The caller **overrides**:
- `actionOverride` (required — the new scene's action)
- `cameraOverride`, `settingOverride` (optional)

**Request body:**
```json
{
  "title":           "The Breach",
  "actionOverride":  "Elena kicks open the gate and enters the courtyard",
  "cameraOverride":  "wide establishing shot",
  "settingOverride": "interior courtyard, early morning mist",
  "model":           "violet-realism",
  "aspectRatio":     "16:9",
  "duration":        8,
  "quality":         "high",
  "useLastFrame":    true,
  "idempotencyKey":  "uuid"
}
```

**`useLastFrame: true`:** If the previous scene has a selected generation job,
the system attempts to use its last-frame thumbnail as the `first_frame` reference
image for the new generation, creating visual continuity between scenes.

**Response — 201:** CreateJobResponseDTO (same as `POST /open/v1/jobs`)
```json
{
  "data": {
    "jobId":            "uuid",
    "status":           "queued",
    "estimatedCredits": null,
    "createdAt":        "2026-10-12T09:00:00Z"
  },
  "requestId": "..."
}
```

**Error codes:**

| HTTP | code | Cause |
|---|---|---|
| 404 | `NOT_FOUND` | Previous scene not found or belongs to another user |
| 422 | `MODEL_NOT_SUPPORTED` | Selected model doesn't support image-to-video |
| 429 | `RATE_LIMITED` | Too many concurrent jobs |
| 502 | `PROVIDER_ERROR` | BytePlus returned an error |

---

## Generate for Scene

### POST /open/v1/dna/scenes/:id/generate

Submit a generation job for an existing scene without creating a new scene.
The scene's current character profile, style profile, and tags are read at
submission time and snapshotted into the job's `effectiveInputs`.

**Request body:** same as `POST /open/v1/jobs`

---

## Generation Jobs (Extended)

### POST /open/v1/jobs

Create a generation job. Extended to accept VIOLET DNA references.

**Request body:**
```json
{
  "prompt":             "Elena charges toward the gate",
  "mode":               "text",
  "model":              "violet-realism",
  "aspectRatio":        "16:9",
  "duration":           8,
  "quality":            "high",
  "enhancePrompt":      false,
  "referenceImageId":   null,
  "sceneId":            "uuid",
  "characterProfileId": "uuid",
  "styleProfileId":     "uuid",
  "tagIds":             ["uuid"],
  "idempotencyKey":     "uuid"
}
```

When `sceneId`, `characterProfileId`, or `styleProfileId` are provided,
the Prompt Compiler assembles the effective prompt from those profiles plus
the raw `prompt` field. The compiled prompt is stored in `compiledPrompt`.

**Response — 201:**
```json
{
  "data": {
    "jobId":            "uuid",
    "status":           "queued",
    "estimatedCredits": null,
    "createdAt":        "2026-10-12T09:00:00Z"
  },
  "requestId": "..."
}
```

---

### GET /open/v1/jobs/:jobId

Get generation job status and result.

**Response — 200:**
```json
{
  "data": {
    "jobId":              "uuid",
    "status":             "succeeded",
    "progress":           100,
    "currentStep":        null,
    "model":              "violet-realism",
    "aspectRatio":        "16:9",
    "duration":           8,
    "quality":            "high",
    "creditsCharged":     null,
    "result": {
      "thumbnailUrl": "https://cdn.violetstudio.app/...",
      "mediaUrl":     "https://cdn.violetstudio.app/...",
      "mediaType":    "video",
      "expiresAt":    null
    },
    "error":              null,
    "sceneId":            "uuid",
    "characterProfileId": "uuid",
    "styleProfileId":     "uuid",
    "compiledPrompt":     "Elena charges toward the gate, tall woman with sharp features...",
    "createdAt":          "2026-10-12T09:00:00Z",
    "updatedAt":          "2026-10-12T09:02:00Z",
    "completedAt":        "2026-10-12T09:02:00Z"
  },
  "requestId": "..."
}
```

**Job statuses:**

| Status | Description |
|---|---|
| `queued` | Persisted; not yet submitted to BytePlus |
| `submitted` | BytePlus task created (provider status: `queued`) |
| `processing` | BytePlus is generating (provider status: `running`) |
| `succeeded` | Complete; `result` is populated |
| `failed` | Failed; `error` is populated |
| `cancelled` | User-cancelled |

---

### DELETE /open/v1/jobs/:jobId — Cancel Job

**Response — 200:**
```json
{ "data": { "jobId": "uuid", "status": "cancelled" }, "requestId": "..." }
```

---

## Prompt Compiler — Precedence Rules

The prompt compiler assembles inputs in this order:

```
1. Scene actionOverride   (or scene description when no override)
2. Character appearance   (appearance, hairstyle, clothing, accessories, definingFeatures)
3. Style description      (visualStyle, colorPalette, cameraLanguage¹, lighting, mood, qualityNotes)
4. Positive tags          (all non-negative_constraint tags, comma-separated)
5. Character consistency notes
```

`¹` Camera: scene `cameraOverride` replaces `styleProfile.cameraLanguage` when present.

**Negative constraint tags** are isolated into `NegativeInstructions` (not injected into the prompt text).

**Immutability:** The full effective inputs are serialised to JSONB in
`video_generation_jobs.effective_inputs` at job creation. Editing profiles
after submission does NOT change historical job records.

---

## Model Registry

| Internal ID | BytePlus Model | Max Duration | Audio | IMG→Video |
|---|---|---|---|---|
| `violet-motion-2` | `dreamina-seedance-2-0-fast-260128` | 15 s | No | Yes |
| `violet-cinema` | `dreamina-seedance-2-0-260128` | 15 s | No | Yes |
| `violet-fast` | `dreamina-seedance-2-0-fast-260128` | 15 s | No | Yes |
| `violet-realism` | `dreamina-seedance-2-5-260628` | 30 s | Yes | Yes |

> **Verify** model IDs against live BytePlus console before production deployment.

---

## Rate Limits

| Scope | Limit |
|---|---|
| Concurrent active jobs per user | 3 (configurable via `MAX_CONCURRENT_JOBS_PER_USER`) |
| Character profiles per user | unlimited |
| Style profiles per user | unlimited |
| Scenes per user | unlimited |

---

## Error Code Reference

| Code | HTTP | Meaning |
|---|---|---|
| `UNAUTHORIZED` | 401 | Missing or invalid JWT |
| `VALIDATION_ERROR` | 400 | Field validation failed; see `details` |
| `NOT_FOUND` | 404 | Resource not found or belongs to another user |
| `CONFLICT` | 409 | State conflict (e.g. cancel a completed job) |
| `IMAGE_REQUIRED` | 422 | Mode requires a reference image |
| `IMAGE_EXPIRED` | 422 | Referenced upload has expired |
| `MODEL_NOT_SUPPORTED` | 422 | Model does not support the requested mode |
| `RATE_LIMITED` | 429 | Too many concurrent jobs |
| `PROVIDER_ERROR` | 502 | BytePlus returned an unexpected error |
| `INTERNAL_ERROR` | 500 | Unexpected server error |

---

## Known Limitations

1. **Object storage not wired.** `output_media_key` currently stores the raw BytePlus
   URL. A storage service implementation is required to download and persist media.
   The BytePlus URL expires in ~24 hours.

2. **Signed URLs.** The `mediaUrl` in job results is not yet converted to a CDN signed
   URL. The storage service abstraction is defined; implementation is deferred.

3. **Upload validation.** Magic-byte file validation for uploads is not yet implemented.
   The endpoint stub exists at `POST /open/v1/uploads` (referenced in the original contract).

4. **Tag lookup by ID.** The Prompt Compiler's tag assembly currently accepts pre-resolved
   `models.DNATag` slices. A `TagRepository.FindByIDs` method is needed to load tags
   from the request `tagIds` field.

5. **Credit system.** Credit reservation and charging logic is not implemented.
   The `estimated_credits` and `charged_credits` columns are present in the schema.

6. **Visual character consistency is not guaranteed.** The system provides structural
   consistency (same prompt tokens, same reference images, scene chaining). Pixel-level
   character identity requires the provider to support consistent character IDs or
   reference-image conditioning — verify BytePlus capability per model.
