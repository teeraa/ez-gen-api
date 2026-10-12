# VIOLET STUDIO — Frontend API Contract

> Version: 1.0.0
> Last updated: 2026-10-12
> Base URL (local): `http://localhost:8080`
> Base URL (prod): `https://api.violetstudio.app` *(TBD)*
> Auth: **All endpoints require** `Authorization: Bearer <supabase-jwt>`

---

## Table of Contents

1. [Response Envelope](#response-envelope)
2. [Error Codes](#error-codes)
3. [Generation Jobs](#generation-jobs)
4. [File Uploads](#file-uploads)
5. [VIOLET DNA — Tags](#violet-dna--tags)
6. [VIOLET DNA — Character Profiles](#violet-dna--character-profiles)
7. [VIOLET DNA — Style Profiles](#violet-dna--style-profiles)
8. [VIOLET DNA — Scenes](#violet-dna--scenes)
9. [Continue Scene](#continue-scene)
10. [Models Reference](#models-reference)
11. [TypeScript Types](#typescript-types)

---

## Response Envelope

ทุก response ใช้รูปแบบเดียวกัน

### Success
```json
{
  "data": { ... },
  "requestId": "550e8400-e29b-41d4-a716-446655440000"
}
```

### List (paginated)
```json
{
  "data": {
    "items": [ ... ],
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
  "message": "validation failed",
  "details": [
    { "field": "prompt", "message": "must be at most 2000 characters" }
  ],
  "requestId": "..."
}
```

> `details` มีเฉพาะ `VALIDATION_ERROR` เท่านั้น

---

## Error Codes

| Code | HTTP | ความหมาย |
|------|------|-----------|
| `UNAUTHORIZED` | 401 | JWT หมดอายุ หรือไม่มี header |
| `VALIDATION_ERROR` | 400 | field ไม่ผ่าน validation (ดู `details`) |
| `NOT_FOUND` | 404 | ไม่พบ resource หรือเป็นของ user อื่น |
| `CONFLICT` | 409 | state conflict เช่น cancel job ที่ complete ไปแล้ว |
| `IMAGE_REQUIRED` | 422 | mode นี้ต้องการ referenceImageId |
| `IMAGE_EXPIRED` | 422 | upload หมดอายุแล้ว |
| `MODEL_NOT_SUPPORTED` | 422 | model ไม่รองรับ mode นี้ |
| `RATE_LIMITED` | 429 | job active เกิน 3 งานพร้อมกัน |
| `PROVIDER_ERROR` | 502 | BytePlus ตอบ error กลับมา |
| `INTERNAL_ERROR` | 500 | server error |

---

## Generation Jobs

### POST `/open/v1/jobs` — สร้าง generation job

**Request body:**
```json
{
  "prompt":             "A warrior stands on a hill at golden hour",
  "mode":               "text",
  "model":              "violet-motion-2",
  "aspectRatio":        "16:9",
  "duration":           5,
  "quality":            "standard",
  "enhancePrompt":      false,
  "referenceImageId":   null,
  "sceneId":            null,
  "characterProfileId": null,
  "styleProfileId":     null,
  "tagIds":             [],
  "idempotencyKey":     "550e8400-e29b-41d4-a716-446655440000"
}
```

| Field | Type | Required | Validation |
|-------|------|----------|------------|
| `prompt` | string | ✅ | 1–2000 chars |
| `mode` | string | ✅ | `text` \| `image_video` \| `image_image` |
| `model` | string | ✅ | ดู [Models Reference](#models-reference) |
| `aspectRatio` | string | ✅ | `16:9` `9:16` `1:1` `4:3` `3:4` `21:9` |
| `duration` | int | ✅ | 4–30 (วินาที) |
| `quality` | string | ✅ | `standard` \| `high` |
| `enhancePrompt` | bool | — | default `false` |
| `referenceImageId` | uuid \| null | — | required เมื่อ mode = `image_video` หรือ `image_image` |
| `sceneId` | uuid \| null | — | ผูกกับ scene ที่มีอยู่ |
| `characterProfileId` | uuid \| null | — | ใช้ character profile นี้ compile prompt |
| `styleProfileId` | uuid \| null | — | ใช้ style profile นี้ compile prompt |
| `tagIds` | uuid[] | — | tag IDs เพิ่มเติม |
| `idempotencyKey` | uuid \| null | — | ส่งซ้ำแล้ว return job เดิม (ไม่ตัดเครดิตซ้ำ) |

**Response — 201:**
```json
{
  "data": {
    "jobId":            "3fa85f64-5717-4562-b3fc-2c963f66afa6",
    "status":           "queued",
    "estimatedCredits": null,
    "createdAt":        "2026-10-12T09:00:00Z"
  },
  "requestId": "..."
}
```

> **Idempotency:** ถ้า `idempotencyKey` ซ้ำกับ job ที่มีอยู่ จะ return job เดิม (HTTP 200 ไม่ใช่ 201)

---

### GET `/open/v1/jobs` — รายการ jobs

**Query params:** `?page=1&limit=20`

**Response — 200:** list of `JobDTO` (ดูโครงสร้างด้านล่าง)

---

### GET `/open/v1/jobs/:jobId` — ดูสถานะ job

**Response — 200:**
```json
{
  "data": {
    "jobId":              "3fa85f64-5717-4562-b3fc-2c963f66afa6",
    "status":             "processing",
    "progress":           45,
    "currentStep":        null,
    "model":              "violet-motion-2",
    "aspectRatio":        "16:9",
    "duration":           5,
    "quality":            "standard",
    "creditsCharged":     null,
    "result":             null,
    "error":              null,
    "sceneId":            null,
    "characterProfileId": null,
    "styleProfileId":     null,
    "compiledPrompt":     "A warrior stands on a hill at golden hour",
    "createdAt":          "2026-10-12T09:00:00Z",
    "updatedAt":          "2026-10-12T09:01:00Z",
    "completedAt":        null
  },
  "requestId": "..."
}
```

**Job statuses — Polling flow:**

```
queued → submitted → processing → succeeded
                               ↘ failed
                               ↘ cancelled
```

| Status | แปลว่า | ควร poll ต่อไหม |
|--------|--------|----------------|
| `queued` | รอส่ง BytePlus | ✅ ทุก 2 วินาที |
| `submitted` | BytePlus รับงานแล้ว | ✅ ทุก 2 วินาที |
| `processing` | กำลัง generate | ✅ ทุก 2 วินาที |
| `succeeded` | เสร็จแล้ว `result` มีข้อมูล | ❌ หยุด |
| `failed` | ล้มเหลว `error` มีข้อมูล | ❌ หยุด |
| `cancelled` | ถูก cancel | ❌ หยุด |

**เมื่อ `status = succeeded`:**
```json
"result": {
  "thumbnailUrl": "https://...",
  "mediaUrl":     "https://...",
  "mediaType":    "video",
  "expiresAt":    null
}
```

**เมื่อ `status = failed`:**
```json
"error": {
  "code":    "PROVIDER_ERROR",
  "message": "Generation failed due to content policy"
}
```

---

### DELETE `/open/v1/jobs/:jobId` — Cancel job

**Response — 200:**
```json
{
  "data": { "jobId": "...", "status": "cancelled" },
  "requestId": "..."
}
```

**Errors:**
- `404 NOT_FOUND` — ไม่พบ job
- `409 CONFLICT` — job อยู่ใน terminal state แล้ว

---

## File Uploads

### POST `/open/v1/uploads` — อัพโหลด reference image

> ใช้ก่อนสร้าง job ที่ mode = `image_video` หรือ `image_image`

**Request:** `Content-Type: multipart/form-data`

```
file: <binary>   (PNG, JPG, หรือ WEBP, ไม่เกิน 8 MB)
```

**Response — 201:**
```json
{
  "data": {
    "uploadId":  "3fa85f64-5717-4562-b3fc-2c963f66afa6",
    "filename":  "reference.png",
    "sizeBytes": 204800,
    "mimeType":  "image/png",
    "expiresAt": "2026-10-13T09:00:00Z"
  },
  "requestId": "..."
}
```

> upload มีอายุ **24 ชั่วโมง** หลังจากนั้น `referenceImageId` นี้จะ invalid

**Errors:**

| HTTP | Code | Cause |
|------|------|-------|
| 400 | `INVALID_FILE_TYPE` | ไม่ใช่ PNG/JPG/WEBP |
| 413 | `FILE_TOO_LARGE` | เกิน 8 MB |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | Content-Type header ไม่ตรง |
| 422 | `INVALID_IMAGE` | ไฟล์เสีย decode ไม่ได้ |

---

## VIOLET DNA — Tags

### GET `/open/v1/dna/tags` — รายการ tags ทั้งหมด

**Query params:** `?category=visual_style` *(optional)*

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

### GET `/open/v1/dna/tags/categories` — รายการ categories

**Response — 200:**
```json
{
  "data": [
    "character",
    "visual_style",
    "environment",
    "lighting_color",
    "camera",
    "motion",
    "mood",
    "audio",
    "quality",
    "negative_constraint"
  ],
  "requestId": "..."
}
```

**Tag categories:**

| Category | ใช้สำหรับ |
|----------|-----------|
| `character` | คุณสมบัติตัวละคร |
| `visual_style` | สไตล์ภาพรวม (photorealistic, anime, cinematic…) |
| `environment` | สภาพแวดล้อม (urban_night, ancient_forest…) |
| `lighting_color` | แสงและสี (golden_hour, neon_purple…) |
| `camera` | การเคลื่อนกล้อง (slow_tracking, drone_aerial…) |
| `motion` | ลักษณะการเคลื่อนไหว (slow_motion, time_lapse…) |
| `mood` | อารมณ์ภาพ (mysterious, epic, joyful…) |
| `audio` | เสียง (ambient_score, no_music…) |
| `quality` | คุณภาพ output (high_detail, 4k_ultra…) |
| `negative_constraint` | สิ่งที่ **ไม่ต้องการ** (avoid_text, avoid_watermark…) |

> `negative_constraint` tags จะ**ไม่**ถูก inject เข้า prompt text แต่จะส่งเป็น negative instructions แยก

---

## VIOLET DNA — Character Profiles

### POST `/open/v1/dna/characters` — สร้าง character profile

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
  "consistencyNotes": "Always confident posture, never shows fear",
  "tagIds":           ["uuid-of-cinematic-tag"]
}
```

| Field | Type | Required | Max |
|-------|------|----------|-----|
| `name` | string | ✅ | 100 chars |
| `description` | string | — | 1000 chars |
| `appearance` | string | — | 500 chars |
| `hairstyle` | string | — | 200 chars |
| `clothing` | string | — | 300 chars |
| `accessories` | string | — | 300 chars |
| `definingFeatures` | string | — | 500 chars |
| `consistencyNotes` | string | — | 1000 chars |
| `tagIds` | uuid[] | — | — |

**Response — 201:** `CharacterProfileDTO`

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
    "tags":             [ { ...TagDTO } ],
    "createdAt":        "2026-10-12T09:00:00Z",
    "updatedAt":        "2026-10-12T09:00:00Z"
  },
  "requestId": "..."
}
```

> `version` เพิ่มขึ้นทุกครั้งที่ update — job ที่ generate ไปแล้วจะ snapshot version ณ ตอนนั้นไว้ ไม่เปลี่ยนตาม profile

---

### GET `/open/v1/dna/characters` — รายการ character profiles

**Query:** `?page=1&limit=20`

**Response — 200:** paginated list of `CharacterProfileDTO`

---

### GET `/open/v1/dna/characters/:id` — ดู character profile

**Response — 200:** `CharacterProfileDTO`

---

### PUT `/open/v1/dna/characters/:id` — แก้ไข character profile

ส่งเฉพาะ field ที่ต้องการเปลี่ยน (partial update)

**Request body:** (ทุก field optional)
```json
{
  "clothing": "silver armour with blue trim",
  "tagIds":   ["uuid1", "uuid2"]
}
```

> การส่ง `tagIds` จะ **แทนที่** tag ทั้งหมดของ profile นั้น ไม่ใช่ append

**Response — 200:** `CharacterProfileDTO` (updated)

---

### DELETE `/open/v1/dna/characters/:id` — ลบ character profile

**Response — 204** (no body)

---

## VIOLET DNA — Style Profiles

### POST `/open/v1/dna/styles` — สร้าง style profile

**Request body:**
```json
{
  "name":           "Violet Noir",
  "description":    "Dark neo-noir aesthetic with purple tones",
  "visualStyle":    "cinematic",
  "colorPalette":   "deep violet and gold",
  "lighting":       "low-key side lighting",
  "mood":           "mysterious",
  "cameraLanguage": "slow dolly push-in",
  "qualityNotes":   "maximum detail, sharp focus on subject",
  "tagIds":         ["uuid"]
}
```

| Field | Type | Required | Max |
|-------|------|----------|-----|
| `name` | string | ✅ | 100 chars |
| `description` | string | — | 1000 chars |
| `visualStyle` | string | — | 200 chars |
| `colorPalette` | string | — | 200 chars |
| `lighting` | string | — | 200 chars |
| `mood` | string | — | 200 chars |
| `cameraLanguage` | string | — | 300 chars |
| `qualityNotes` | string | — | 300 chars |
| `tagIds` | uuid[] | — | — |

**Response — 201:** `StyleProfileDTO`

```json
{
  "data": {
    "id":             "uuid",
    "name":           "Violet Noir",
    "description":    "...",
    "visualStyle":    "cinematic",
    "colorPalette":   "deep violet and gold",
    "lighting":       "...",
    "mood":           "mysterious",
    "cameraLanguage": "slow dolly push-in",
    "qualityNotes":   "...",
    "tags":           [ { ...TagDTO } ],
    "createdAt":      "2026-10-12T09:00:00Z",
    "updatedAt":      "2026-10-12T09:00:00Z"
  },
  "requestId": "..."
}
```

### GET `/open/v1/dna/styles` — รายการ (paginated)
### GET `/open/v1/dna/styles/:id` — ดู single
### PUT `/open/v1/dna/styles/:id` — แก้ไข (partial update)
### DELETE `/open/v1/dna/styles/:id` — ลบ → 204

---

## VIOLET DNA — Scenes

### POST `/open/v1/dna/scenes` — สร้าง scene

Scene คือหน่วยของ creative brief — ผูก character + style profile แล้วเพิ่ม override สำหรับ scene นั้น

**Request body:**
```json
{
  "title":              "The Gate Assault",
  "description":        "Elena charges toward the castle gate at dawn",
  "characterProfileId": "uuid",
  "styleProfileId":     "uuid",
  "previousSceneId":    null,
  "sceneOrder":         1,
  "actionOverride":     "Elena raises her sword and charges forward",
  "cameraOverride":     "low-angle tracking shot",
  "settingOverride":    "castle gate at dawn, cobblestones, thick fog",
  "tagIds":             ["uuid"]
}
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `title` | string | — | max 200 |
| `description` | string | — | max 2000 |
| `characterProfileId` | uuid \| null | — | — |
| `styleProfileId` | uuid \| null | — | — |
| `previousSceneId` | uuid \| null | — | สำหรับ scene chain |
| `sceneOrder` | int | — | default 0, ใช้เรียงลำดับ |
| `actionOverride` | string | — | แทน description เป็น primary action |
| `cameraOverride` | string | — | override `styleProfile.cameraLanguage` |
| `settingOverride` | string | — | override environment |
| `tagIds` | uuid[] | — | — |

**Prompt compilation precedence (สำคัญ):**

```
1. actionOverride  →  scene description  (ถ้าไม่มี action override)
2. Character appearance (appearance, hairstyle, clothing, accessories, definingFeatures)
3. Style  (visualStyle, colorPalette, cameraOverride → cameraLanguage, lighting, mood)
4. Positive tags (comma-separated)
5. Character consistencyNotes
```

**Response — 201:** `SceneDTO`

```json
{
  "data": {
    "id":                 "uuid",
    "title":              "The Gate Assault",
    "description":        "...",
    "characterProfileId": "uuid",
    "styleProfileId":     "uuid",
    "previousSceneId":    null,
    "selectedJobId":      null,
    "sceneOrder":         1,
    "tags":               [ { ...TagDTO } ],
    "createdAt":          "2026-10-12T09:00:00Z",
    "updatedAt":          "2026-10-12T09:00:00Z"
  },
  "requestId": "..."
}
```

---

### GET `/open/v1/dna/scenes` — รายการ scenes

**Query:** `?page=1&limit=20`
ผลลัพธ์เรียงตาม `sceneOrder ASC, createdAt DESC`

---

### GET `/open/v1/dna/scenes/:id` — ดู scene (พร้อม profiles + tags)

---

### PUT `/open/v1/dna/scenes/:id` — แก้ไข scene (partial)

```json
{
  "actionOverride": "Elena kicks down the gate",
  "cameraOverride": "extreme wide shot",
  "tagIds": ["uuid1"]
}
```

---

### DELETE `/open/v1/dna/scenes/:id` — ลบ scene → 204

---

### PUT `/open/v1/dna/scenes/:id/select-output/:jobId` — เลือก output ของ scene

ใช้บอกว่า job ไหนเป็น "output ที่เลือก" สำหรับ scene นี้ (แสดงใน feed / ใช้เป็น last frame ใน Continue Scene)

**Response — 200:**
```json
{
  "data": {
    "sceneId":       "uuid",
    "selectedJobId": "uuid"
  },
  "requestId": "..."
}
```

---

## Continue Scene

### POST `/open/v1/dna/scenes/:id/continue` — ต่อ scene

สร้าง scene ใหม่ที่ต่อจาก scene นี้ แล้วส่ง generation job ทันที

**สิ่งที่ inherit จาก scene ก่อนหน้า:**
- `characterProfileId`
- `styleProfileId`
- tags ทั้งหมด

**สิ่งที่ FE กำหนดเองสำหรับ scene ใหม่:**
- `actionOverride` (required — action ของ scene ถัดไป)
- `cameraOverride`, `settingOverride` (optional)
- model, aspectRatio, duration, quality

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

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `actionOverride` | string | ✅ | 1–2000 chars |
| `title` | string | — | max 200 |
| `cameraOverride` | string | — | max 200 |
| `settingOverride` | string | — | max 300 |
| `model` | string | ✅ | ดู Models Reference |
| `aspectRatio` | string | ✅ | `16:9` `9:16` `1:1` `4:3` `21:9` |
| `duration` | int | ✅ | 4–30 วินาที |
| `quality` | string | ✅ | `standard` \| `high` |
| `useLastFrame` | bool | — | `true` = ใช้ last frame ของ scene ก่อนหน้าเป็น first frame (visual continuity) |
| `idempotencyKey` | uuid | — | — |

**Response — 201:** `CreateJobResponseDTO` (เหมือน POST /open/v1/jobs)

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

จากนั้น poll `/open/v1/jobs/:jobId` เหมือนปกติ

---

### POST `/open/v1/dna/scenes/:id/generate` — generate job สำหรับ scene ที่มีอยู่

ส่ง job สำหรับ scene โดยไม่สร้าง scene ใหม่ ใช้สำหรับ re-generate scene เดิม

**Request body:** เหมือน `POST /open/v1/jobs`

**Response — 201:** `CreateJobResponseDTO`

---

## Models Reference

| model ID | ชื่อ UI | max duration | Audio | IMG→Video |
|----------|---------|-------------|-------|-----------|
| `violet-motion-2` | Violet Motion 2.0 | 15 วินาที | ❌ | ✅ |
| `violet-cinema` | Violet Cinema | 15 วินาที | ❌ | ✅ |
| `violet-fast` | Violet Fast | 15 วินาที | ❌ | ✅ |
| `violet-realism` | Violet Realism | 30 วินาที | ✅ | ✅ |

**quality → resolution mapping:**

| quality | resolution ที่ส่ง BytePlus |
|---------|--------------------------|
| `standard` | 720p |
| `high` | 1080p |

---

## TypeScript Types

```typescript
// ── Shared ────────────────────────────────────────────────────────────────────

interface ApiResponse<T> {
  data: T
  requestId: string
}

interface ApiError {
  code: string
  message: string
  details?: { field: string; message: string }[]
  requestId: string
}

interface PaginatedResponse<T> {
  items: T[]
  total: number
  page: number
  limit: number
}

// ── Tags ──────────────────────────────────────────────────────────────────────

type TagCategory =
  | 'character' | 'visual_style' | 'environment' | 'lighting_color'
  | 'camera' | 'motion' | 'mood' | 'audio' | 'quality' | 'negative_constraint'

interface TagDTO {
  id: string
  slug: string
  name: string
  category: TagCategory
  description: string
  sortOrder: number
  createdAt: string
}

// ── Character Profile ─────────────────────────────────────────────────────────

interface CharacterProfileDTO {
  id: string
  name: string
  description: string
  appearance: string
  hairstyle: string
  clothing: string
  accessories: string
  definingFeatures: string
  consistencyNotes: string
  version: number
  tags: TagDTO[]
  createdAt: string
  updatedAt: string
}

interface CreateCharacterProfileRequest {
  name: string
  description?: string
  appearance?: string
  hairstyle?: string
  clothing?: string
  accessories?: string
  definingFeatures?: string
  consistencyNotes?: string
  tagIds?: string[]
}

// ── Style Profile ─────────────────────────────────────────────────────────────

interface StyleProfileDTO {
  id: string
  name: string
  description: string
  visualStyle: string
  colorPalette: string
  lighting: string
  mood: string
  cameraLanguage: string
  qualityNotes: string
  tags: TagDTO[]
  createdAt: string
  updatedAt: string
}

interface CreateStyleProfileRequest {
  name: string
  description?: string
  visualStyle?: string
  colorPalette?: string
  lighting?: string
  mood?: string
  cameraLanguage?: string
  qualityNotes?: string
  tagIds?: string[]
}

// ── Scene ─────────────────────────────────────────────────────────────────────

interface SceneDTO {
  id: string
  title: string
  description: string
  characterProfileId: string | null
  styleProfileId: string | null
  previousSceneId: string | null
  selectedJobId: string | null
  sceneOrder: number
  tags: TagDTO[]
  createdAt: string
  updatedAt: string
}

interface CreateSceneRequest {
  title?: string
  description?: string
  characterProfileId?: string
  styleProfileId?: string
  previousSceneId?: string
  sceneOrder?: number
  actionOverride?: string
  cameraOverride?: string
  settingOverride?: string
  tagIds?: string[]
}

// ── Jobs ──────────────────────────────────────────────────────────────────────

type JobStatus = 'queued' | 'submitted' | 'processing' | 'succeeded' | 'failed' | 'cancelled'
type JobMode = 'text' | 'image_video' | 'image_image'
type ModelId = 'violet-motion-2' | 'violet-cinema' | 'violet-fast' | 'violet-realism'
type AspectRatio = '16:9' | '9:16' | '1:1' | '4:3' | '3:4' | '21:9'
type Quality = 'standard' | 'high'

interface JobResult {
  thumbnailUrl: string
  mediaUrl: string
  mediaType: 'video' | 'image'
  expiresAt: string | null
}

interface JobError {
  code: string
  message: string
}

interface JobDTO {
  jobId: string
  status: JobStatus
  progress: number
  currentStep: string | null
  model: ModelId
  aspectRatio: AspectRatio
  duration: number
  quality: Quality
  creditsCharged: number | null
  result: JobResult | null
  error: JobError | null
  sceneId: string | null
  characterProfileId: string | null
  styleProfileId: string | null
  compiledPrompt: string | null
  createdAt: string
  updatedAt: string
  completedAt: string | null
}

interface CreateJobRequest {
  prompt: string
  mode: JobMode
  model: ModelId
  aspectRatio: AspectRatio
  duration: number
  quality: Quality
  enhancePrompt?: boolean
  referenceImageId?: string | null
  sceneId?: string | null
  characterProfileId?: string | null
  styleProfileId?: string | null
  tagIds?: string[]
  idempotencyKey?: string
}

interface CreateJobResponse {
  jobId: string
  status: JobStatus
  estimatedCredits: number | null
  createdAt: string
}

// ── Continue Scene ────────────────────────────────────────────────────────────

interface ContinueSceneRequest {
  title?: string
  actionOverride: string          // required
  cameraOverride?: string
  settingOverride?: string
  model: ModelId
  aspectRatio: AspectRatio
  duration: number
  quality: Quality
  useLastFrame?: boolean
  idempotencyKey?: string
}

// ── Upload ────────────────────────────────────────────────────────────────────

interface UploadResponse {
  uploadId: string
  filename: string
  sizeBytes: number
  mimeType: string
  expiresAt: string
}
```

---

## Typical Flows

### Flow 1: Text-to-Video (basic)

```
POST /open/v1/jobs  { prompt, mode:"text", model, aspectRatio, duration, quality }
  → { jobId, status:"queued" }

loop every 2s:
  GET /open/v1/jobs/:jobId
  → status: queued → submitted → processing → succeeded
  → result.mediaUrl ✅
```

### Flow 2: Image-to-Video

```
POST /open/v1/uploads  (multipart/form-data)
  → { uploadId }

POST /open/v1/jobs  { mode:"image_video", referenceImageId: uploadId, ... }
  → { jobId }

GET /open/v1/jobs/:jobId  (poll until succeeded)
```

### Flow 3: VIOLET DNA — Character + Style

```
POST /open/v1/dna/characters  { name, appearance, clothing, ... }
  → { id: charId }

POST /open/v1/dna/styles  { name, visualStyle, cameraLanguage, ... }
  → { id: styleId }

POST /open/v1/dna/scenes  { characterProfileId: charId, styleProfileId: styleId, description: "..." }
  → { id: sceneId }

POST /open/v1/jobs  { sceneId, characterProfileId: charId, styleProfileId: styleId, ... }
  → { jobId }

PUT /open/v1/dna/scenes/:sceneId/select-output/:jobId
```

### Flow 4: Continue Scene (visual continuity)

```
// Scene 1 เสร็จแล้ว selectedJobId ถูก set
POST /open/v1/dna/scenes/:scene1Id/continue
  {
    actionOverride: "Elena enters the courtyard",
    useLastFrame: true,
    model: "violet-realism",
    ...
  }
  → { jobId }   ← job ของ scene 2

GET /open/v1/jobs/:jobId  (poll)
```

---

*Document generated: 2026-10-12 | Backend: ez-gen-api | Contract owner: Backend Team*
