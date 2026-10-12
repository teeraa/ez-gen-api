# Skill: reference-assets-and-storage

Handle all interactions with object storage: upload validation, pre-upload storage,
generated output persistence (downloading provider URLs before they expire), and
signed URL generation for client delivery.

## When to Invoke

- Implementing `POST /open/v1/uploads` (reference image upload)
- Implementing the output media download step after a job succeeds
- Implementing `GET /open/v1/projects/:id` (generate signed URL for response)
- Adding a new object storage provider
- Debugging expired media URLs in production

---

## Two Distinct Storage Flows

### Flow A — Reference Image Upload (before generation)

```
Client → POST /open/v1/uploads (multipart) → validate → store in S3/R2/TOS
                                           → return uploadId + expiresAt (24h)
                                           → store record in uploads table
```

### Flow B — Generated Output Persistence (after generation succeeds)

```
Poller detects status == "succeeded"
    → Download from response.content.video_url (BytePlus, expires ~24h)
    → Upload to application object storage
    → Store object storage KEY in video_generation_jobs.output_media_key
    → NEVER store the BytePlus URL
```

---

## Storage Abstraction

`apps/services/storage/storage.go`:

```go
package storage

// Client is the storage interface. Implement once per provider.
type Client interface {
    // Upload stores bytes at the given key. Returns the canonical storage key.
    Upload(ctx context.Context, key string, content io.Reader, contentType string) error

    // Download fetches an object and returns its content.
    Download(ctx context.Context, key string) (io.ReadCloser, error)

    // SignedURL returns a pre-signed URL valid for the given duration.
    SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error)

    // Delete removes an object. No-op if the key does not exist.
    Delete(ctx context.Context, key string) error
}

// KeyForUpload generates a deterministic storage key for a user upload.
func KeyForUpload(userID, uploadID string) string {
    return fmt.Sprintf("uploads/%s/%s", userID, uploadID)
}

// KeyForOutput generates a storage key for generated output.
func KeyForOutput(userID, jobID, ext string) string {
    return fmt.Sprintf("outputs/%s/%s.%s", userID, jobID, ext)
}

// KeyForThumbnail generates a storage key for a generated thumbnail.
func KeyForThumbnail(userID, jobID string) string {
    return fmt.Sprintf("thumbs/%s/%s.jpg", userID, jobID)
}
```

### Providers to Implement

| Provider | Env vars | Notes |
|---|---|---|
| AWS S3 | `STORAGE_BUCKET`, `STORAGE_REGION`, `STORAGE_ACCESS_KEY`, `STORAGE_SECRET_KEY` | Production default |
| Cloudflare R2 | `STORAGE_BUCKET`, `STORAGE_ACCOUNT_ID`, `STORAGE_ACCESS_KEY`, `STORAGE_SECRET_KEY` | S3-compatible |
| BytePlus TOS | `STORAGE_BUCKET`, `STORAGE_REGION`, `BYTEPLUS_ACCESSKEY`, `BYTEPLUS_SECRETKEY` | Co-located with ModelArk |
| Local (test) | — | In-memory or temp filesystem — for unit tests only |

Select provider via `STORAGE_PROVIDER` env var. Wire in `pkgs/configs/config.go`.

---

## Upload Handler (Flow A)

`apps/handlers/handler_video_generation/upload.go`:

```go
func (h *Handler) UploadImage(c *fiber.Ctx) error {
    userID := c.Locals("userID").(string)
    rid := c.Locals("requestID").(string)

    file, err := c.FormFile("file")
    if err != nil {
        return c.Status(400).JSON(response.Error("VALIDATION_ERROR", "file field required", nil, rid))
    }

    // Size limit is enforced by Fiber body limit middleware (8 MB)

    src, err := file.Open()
    if err != nil { return internalError(c, rid) }
    defer src.Close()

    // Read first 512 bytes for magic-byte check
    header := make([]byte, 512)
    n, _ := src.Read(header)
    header = header[:n]
    src.Seek(0, io.SeekStart)

    mimeType := detectMIME(header)
    if mimeType == "" {
        return c.Status(400).JSON(response.Error("INVALID_FILE_TYPE",
            "Only PNG, JPG, and WEBP files are accepted", nil, rid))
    }

    result, err := h.facade.UploadReferenceImage(c.Context(), userID, facade_video_generation.UploadInput{
        Reader:      src,
        Filename:    file.Filename,
        Size:        file.Size,
        ContentType: mimeType,
    })
    if err != nil { return mapError(c, err, rid) }
    return c.Status(201).JSON(response.Success(result, rid))
}
```

Magic byte detection:

```go
func detectMIME(header []byte) string {
    switch {
    case len(header) >= 3 && header[0]==0xFF && header[1]==0xD8 && header[2]==0xFF:
        return "image/jpeg"
    case len(header) >= 8 &&
        header[0]==0x89 && header[1]==0x50 && header[2]==0x4E && header[3]==0x47 &&
        header[4]==0x0D && header[5]==0x0A && header[6]==0x1A && header[7]==0x0A:
        return "image/png"
    case len(header) >= 12 &&
        string(header[0:4])=="RIFF" && string(header[8:12])=="WEBP":
        return "image/webp"
    default:
        return ""
    }
}
```

---

## Output Persistence (Flow B)

`apps/facades/facade_video_generation/persist_output.go`:

```go
func (f *facade) PersistOutput(ctx context.Context, job *models.VideoGenerationJob, providerVideoURL string) error {
    // 1. Download from provider (with timeout)
    ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
    defer cancel()

    resp, err := f.httpClient.Get(providerVideoURL)
    if err != nil { return fmt.Errorf("download output: %w", err) }
    defer resp.Body.Close()

    ext := "mp4"
    if job.FileFormat != "" { ext = job.FileFormat }
    key := storage.KeyForOutput(job.UserID.String(), job.ID.String(), ext)

    // 2. Upload to application storage
    if err := f.storage.Upload(ctx, key, resp.Body, "video/mp4"); err != nil {
        return fmt.Errorf("store output: %w", err)
    }

    // 3. Update job with storage key (atomic with status transition)
    return f.repo.TransitionStatus(ctx, job.ID.String(), "processing", "succeeded",
        func(j *models.VideoGenerationJob) {
            j.OutputMediaKey = &key
            j.OutputMediaType = strPtr("video")
            completedAt := time.Now()
            j.CompletedAt = &completedAt
        })
}
```

**Timeout:** Download must complete within 5 minutes. If it fails:
- Mark job `failed` with `failure_code: OUTPUT_DOWNLOAD_FAILED`
- Do NOT retry download automatically — the provider URL is still valid for ~24h.
  The poller's next cycle will retry.

---

## Signed URL Generation

Never store signed URLs in the database. Generate on demand:

```go
func (f *facade) GetJobResult(ctx context.Context, jobID, userID string) (*dto.JobResultDTO, error) {
    job, err := f.repo.FindByID(ctx, jobID, userID)
    if err != nil { return nil, err }

    dto := mapToJobDTO(job)
    if job.OutputMediaKey != nil {
        signedURL, err := f.storage.SignedURL(ctx, *job.OutputMediaKey, 12*time.Hour)
        if err != nil { return nil, fmt.Errorf("sign media URL: %w", err) }
        expiresAt := time.Now().Add(12 * time.Hour).UTC().Format(time.RFC3339)
        dto.Result = &JobResultDTO{
            MediaURL:  signedURL,
            MediaType: *job.OutputMediaType,
            ExpiresAt: expiresAt,
        }
    }
    return dto, nil
}
```

---

## Acceptance Criteria

- [ ] JPEG, PNG, WEBP magic bytes correctly identified; all other types rejected
- [ ] Output download uses a timeout (5 min), not unlimited
- [ ] `output_media_key` stored, never `video_url` from provider
- [ ] Signed URLs generated on demand, not stored
- [ ] Upload records have `expires_at = NOW() + 24h`
- [ ] Local mock storage client available for tests (no S3 credentials required)
- [ ] `go test ./apps/services/storage/...` passes without cloud credentials

---

## Related Skills

- `video-generation-security` — magic byte validation and upload size limits
- `video-generation-lifecycle` — calls `PersistOutput` on job success
- `creative-dna-prompt-compiler` — calls `SignedURL` for reference image ContentItems
- `modelark-integration-testing` — uses local mock storage in tests
