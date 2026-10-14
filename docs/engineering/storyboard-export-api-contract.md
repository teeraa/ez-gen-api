# Storyboard Export API Contract — Frontend Integration

> Date: 2026-10-14
> Status: **Implemented** (baseline + timeline editor extension)
> Backend: Go Fiber (`ez-gen-api`)
> Scope: Merge scene clips into one video with optional per-clip trim, background audio,
> and text overlays (3-position or free placement).

This document describes the endpoints the frontend uses to upload media and to
create/track storyboard exports. All conventions follow the existing
`video-generation-api-contract.md` (same envelope, camelCase, ISO 8601 UTC timestamps).

---

## Overview

An **export** merges the selected output clips of two or more scenes, in order, into a
single MP4. Optionally it mixes a background audio track and burns per-scene text
captions. Processing is asynchronous: create an export, then poll its status until it
reaches a terminal state, then fetch a signed download URL.

```
1. POST /open/v1/uploads            (optional) upload an audio file → uploadId
2. POST /open/v1/exports            queue the export  → exportId (status: queued)
3. GET  /open/v1/exports/:exportId  poll until status = succeeded | failed
4. GET  /open/v1/exports/:exportId/download   → short-lived signed URL
```

A scene is eligible for export only if it has a **selected job** whose status is
`succeeded` with a stored video output. (Set via `PUT /open/v1/dna/scenes/:id/select-output/:jobId`.)

---

## Authentication

All endpoints below require:
```
Authorization: Bearer <jwt-token>
```
Unauthenticated → `401 UNAUTHORIZED`.

---

## 1. Upload Media

Upload a reference image or an audio track. Returns an `uploadId` you pass to later
calls. Uploads expire 24 hours after creation.

```
POST /open/v1/uploads
Content-Type: multipart/form-data
```

| Field | Type | Notes |
|---|---|---|
| `file` | file | Required. The binary file. Field name must be `file`. |

**Accepted types** (validated by magic bytes, not the filename or declared MIME):

| Category | Types | Max size |
|---|---|---|
| Image | JPEG, PNG, WebP | 8 MB |
| Audio | MP3, AAC, WAV, M4A | 50 MB |

### Response `201`
```json
{
  "data": {
    "uploadId": "550e8400-e29b-41d4-a716-446655440000",
    "mimeType": "audio/mpeg",
    "sizeBytes": 3840217,
    "expiresAt": "2026-10-15T09:00:00Z"
  },
  "requestId": "..."
}
```

### Errors
| HTTP | code | When |
|---|---|---|
| 400 | `VALIDATION_ERROR` | Missing `file` field |
| 400 | `INVALID_FILE_TYPE` | File is not a supported image/audio type |
| 413 | `FILE_TOO_LARGE` | File exceeds the size limit for its type |

---

## 2. Create Export

Queues a new storyboard export. Returns immediately with status `queued`; a background
worker performs the merge.

```
POST /open/v1/exports
Content-Type: application/json
```

### Request Body
```json
{
  "sceneIds": [
    "11111111-1111-1111-1111-111111111111",
    "22222222-2222-2222-2222-222222222222"
  ],
  "audioUploadId": "550e8400-e29b-41d4-a716-446655440000",
  "audioVolume": 0.8,
  "textLayers": [
    {
      "sceneId": "11111111-1111-1111-1111-111111111111",
      "text": "Chapter One",
      "position": "bottom",
      "fontSize": 48,
      "color": "FFFFFF"
    }
  ]
}
```

| Field | Type | Required | Rules |
|---|---|---|---|
| `sceneIds` | string[] | yes | 2–20 scene UUIDs, in presentation order. Each must have a selected succeeded job. |
| `audioUploadId` | string\|null | no | UUID of an audio upload. Must be an audio file. |
| `audioVolume` | number\|null | no | Background audio gain. `1.0` = equal to clip audio. Defaults to `1.0`. |
| `textLayers` | object[]\|null | no | Per-scene caption overlays (see below). |

**TextLayer object:**

| Field | Type | Required | Rules |
|---|---|---|---|
| `sceneId` | string | yes | UUID. Must appear in `sceneIds`; layers for other scenes are ignored. |
| `text` | string | yes | 1–200 chars. Shown for the full duration of that scene's clip (unless timed — see extension). |
| `position` | string | no | `top` \| `center` \| `bottom`. Default `bottom`. Ignored when `x`/`y` are set. |
| `fontSize` | number | no | Pixels at a **1080-height reference**; backend scales to the real output height. Default `48`. |
| `color` | string | no | Hex **without** `#`, e.g. `FFFFFF`. Default white. |

### Timeline editor extension (implemented)

Two additive capabilities on top of the baseline body. Both are optional; a baseline body
(`sceneIds` + 3-position text) still works unchanged.

**a) Per-clip trim — `clips` supersedes `sceneIds`.**

```json
{
  "clips": [
    { "sceneId": "1111...", "trimStart": 0.0, "trimEnd": 4.5 },
    { "sceneId": "2222...", "trimEnd": 3.0 }
  ]
}
```

| Field | Type | Required | Rules |
|---|---|---|---|
| `clips` | object[] | — | 2–20 items; supersedes `sceneIds` when present. Each scene at most once. |
| `clips[].sceneId` | string | yes | UUID; must have a selected succeeded video. |
| `clips[].trimStart` | number | no | Seconds from the clip start. Default `0`. |
| `clips[].trimEnd` | number | no | Seconds from the clip start. Default = full duration. Must be `> trimStart`. |

- Send **either** `sceneIds` **or** `clips`. If both are present, `clips` wins.
- Trim units are **seconds** (float). Total export duration is the sum of trimmed clip lengths.
- Trim forces the FFmpeg concat filter (not stream-copy), so every export is re-encoded.

**b) Free text placement + timing — extra `textLayers` fields.**

```json
{
  "textLayers": [
    {
      "sceneId": "1111...",
      "text": "Chapter One",
      "x": 0.5, "y": 0.9, "align": "center",
      "fontSize": 48, "color": "FFFFFF",
      "startTime": 0.0, "endTime": 2.0
    }
  ]
}
```

| Field | Type | Required | Rules |
|---|---|---|---|
| `x` | number | no | Normalized `0..1` of frame width; **centre anchor**. When set with `y`, overrides `position`. |
| `y` | number | no | Normalized `0..1` of frame height; **centre anchor**. |
| `align` | string | no | `left` \| `center` \| `right`. Shifts the anchor horizontally. Default `center`. |
| `startTime` | number | no | Seconds **relative to the clip's trimmed start**. Absent (with `endTime`) = whole clip. |
| `endTime` | number | no | Seconds relative to the trimmed start. Must be `> startTime`; clamped to clip end. |

- `x` and `y` must be supplied **together** to take effect; a lone `x` or `y` falls back to `position`.
- `fontSize` is authored against a 1080-px-tall preview; the backend scales it to the output height.
- Timing is relative to each clip's **trimmed** start, so it lines up after trims are applied.

### Response `201`
```json
{
  "data": {
    "exportId": "99999999-9999-9999-9999-999999999999",
    "status": "queued",
    "createdAt": "2026-10-14T09:00:00Z",
    "completedAt": null,
    "error": null
  },
  "requestId": "..."
}
```

### Errors
| HTTP | code | When |
|---|---|---|
| 400 | `VALIDATION_ERROR` | Body fails validation: fewer than 2 or more than 20 clips, a duplicate scene, a non-UUID, or `trimEnd <= trimStart` |
| 404 | `NOT_FOUND` | A scene or the audio upload does not exist / belongs to another user |
| 422 | `SCENE_HAS_NO_OUTPUT` | A scene has no selected succeeded job with a stored video |
| 400 | `INVALID_FILE_TYPE` | `audioUploadId` references a non-audio upload |

---

## 3. Get Export Status

Poll this until `status` is terminal (`succeeded` or `failed`).

```
GET /open/v1/exports/:exportId
```

### Response `200`
```json
{
  "data": {
    "exportId": "99999999-9999-9999-9999-999999999999",
    "status": "processing",
    "createdAt": "2026-10-14T09:00:00Z",
    "completedAt": null,
    "error": null
  },
  "requestId": "..."
}
```

**Status values:** `queued` → `processing` → `succeeded` | `failed`

On `failed`, `error` holds a short human-readable message; `completedAt` is set.

### Errors
| HTTP | code | When |
|---|---|---|
| 404 | `NOT_FOUND` | Export does not exist / belongs to another user |

**Suggested polling cadence:** every 3–5 s. A typical 5-scene merge completes in
roughly 60–120 s depending on clip length and whether text overlays are applied.

---

## 4. List Exports

```
GET /open/v1/exports?page=1&limit=20
```

| Query | Default | Max |
|---|---|---|
| `page` | 1 | — |
| `limit` | 20 | 50 |

### Response `200`
```json
{
  "data": {
    "items": [
      {
        "exportId": "99999999-9999-9999-9999-999999999999",
        "status": "succeeded",
        "createdAt": "2026-10-14T09:00:00Z",
        "completedAt": "2026-10-14T09:01:30Z",
        "error": null
      }
    ],
    "total": 1,
    "page": 1,
    "limit": 20
  },
  "requestId": "..."
}
```

---

## 5. Download Finished Export

Returns a short-lived signed URL (TTL ~1 hour). The link forces a download with a
filename of `export-<exportId>.mp4`.

```
GET /open/v1/exports/:exportId/download
```

### Response `200`
```json
{
  "data": {
    "url": "https://<project>.supabase.co/storage/v1/object/sign/...&download=export-...mp4",
    "expiresAt": "2026-10-14T10:01:30Z"
  },
  "requestId": "..."
}
```

### Errors
| HTTP | code | When |
|---|---|---|
| 404 | `NOT_FOUND` | Export does not exist / belongs to another user |
| 409 | `EXPORT_NOT_READY` | Export has not finished (no output yet) |

---

## Error Envelope

Every error uses the standard envelope:
```json
{
  "code": "SCENE_HAS_NO_OUTPUT",
  "message": "one or more scenes have no completed video output",
  "requestId": "..."
}
```
`details` is present only for `VALIDATION_ERROR`:
```json
{
  "code": "VALIDATION_ERROR",
  "message": "validation failed",
  "details": [{ "field": "sceneIds", "message": "must have at least 2 items" }],
  "requestId": "..."
}
```

---

## Error Code Reference

| Code | HTTP | Meaning |
|---|---|---|
| `VALIDATION_ERROR` | 400 | Field validation failed |
| `INVALID_FILE_TYPE` | 400 | Upload is not a supported image/audio type |
| `FILE_TOO_LARGE` | 413 | Upload exceeds its size limit |
| `NOT_FOUND` | 404 | Resource missing or owned by another user |
| `SCENE_HAS_NO_OUTPUT` | 422 | A scene lacks a selected succeeded video |
| `EXPORT_NOT_READY` | 409 | Download requested before export finished |
| `UNAUTHORIZED` | 401 | Missing/invalid token |
| `INTERNAL_ERROR` | 500 | Unexpected server error |
