# Security Rules — VIOLET STUDIO Backend

Non-negotiable security rules. Every agent must read this file before writing code.
Any violation blocks the merge.

---

## Rule 1: Secrets Never Leave the Server

```go
// WRONG — secret in log
log.Printf("calling BytePlus with key: %s", os.Getenv("ARK_API_KEY"))

// WRONG — secret in error response
return c.Status(500).JSON(fiber.Map{"error": err.Error()})  // may contain key

// WRONG — secret in test file
const testAPIKey = "ark-abc123..."

// CORRECT — secret only used, never printed
client, err := modelark.NewClient(modelark.ConfigFromEnv())
```

If any secret appears in a log, error message, HTTP response, or committed file:
treat it as compromised and rotate it immediately.

---

## Rule 2: User Isolation Is Non-Negotiable

Every query on user-owned data must filter by `user_id`:

```go
// CORRECT
db.WithContext(ctx).
    Where("id = ? AND user_id = ? AND deleted_at IS NULL", jobID, userID).
    First(&job)

// WRONG — no ownership check
db.WithContext(ctx).Where("id = ?", jobID).First(&job)
```

When a resource exists but belongs to another user, return **HTTP 404**, not 403.
Do not confirm the existence of another user's resources.

---

## Rule 3: userID Comes From JWT, Not the Request

```go
// CORRECT
userID := c.Locals("userID").(string)

// WRONG — user can send any UUID they want
userID := c.Query("userId")
userID := body.UserID
```

The JWT middleware sets `c.Locals("userID")` from the validated token.
Handlers must read from `c.Locals`, never from the request body or query params.

---

## Rule 4: Validate Every Input at the Boundary

```go
// CORRECT: validate before any business logic
if errs := h.validator.Validate(req); errs != nil {
    return c.Status(400).JSON(response.ValidationError(errs, rid))
}

// All request structs must have validate: tags on every field
type CreateJobRequest struct {
    Prompt      string `json:"prompt"  validate:"required,min=1,max=2000"`
    Mode        string `json:"mode"    validate:"required,oneof=text image_video image_image"`
    Duration    int    `json:"duration" validate:"required,oneof=5 10"`
}
```

File uploads: validate Content-Type header + magic bytes + image decode. Not just the extension.

Magic bytes:
- JPEG: first 3 bytes = `FF D8 FF`
- PNG: first 8 bytes = `89 50 4E 47 0D 0A 1A 0A`
- WEBP: bytes 0-3 = `52 49 46 46`, bytes 8-11 = `57 45 42 50`

---

## Rule 5: BytePlus Task IDs Never Leave the Backend

```go
// WRONG — DTO includes provider task ID
type JobResponse struct {
    ProviderTaskID string `json:"providerTaskId"`  // NEVER
}

// CORRECT — only application UUID
type JobResponse struct {
    JobID string `json:"jobId"`  // application UUID only
}
```

The `provider_task_id` column is internal. It is used only for:
- Calling the BytePlus status API
- Logging (at debug level, not info)

---

## Rule 6: Provider Output URLs Are Not Stored

```go
// WRONG — storing BytePlus URL
job.OutputMediaURL = providerResult.VideoURL
repo.Save(ctx, job)

// CORRECT — download and store in application storage
storageKey, err := s.storage.Upload(ctx, providerResult.VideoURL)
job.OutputMediaKey = storageKey  // object storage key
repo.Save(ctx, job)
```

BytePlus output URLs expire in ~24 hours. Store the object storage key.
Generate signed URLs from application storage on-demand when serving to clients.

---

## Rule 7: Error Responses Must Not Expose Internals

```go
// WRONG — raw error in response
return c.Status(500).JSON(fiber.Map{"error": err.Error()})
// err.Error() may contain SQL queries, file paths, or credentials

// CORRECT — generic message + internal logging
log.Printf("internal error processing job %s: %v", jobID, err)
return c.Status(500).JSON(response.Error("INTERNAL_ERROR", "An error occurred", nil, rid))
```

Provider errors from BytePlus: log the full error internally, store a sanitized
version in `failure_message`, never forward the raw payload to the client.

---

## Rule 8: Prompt Logging Is Truncated

```go
// WRONG — full prompt in log (potential data leak, log injection)
log.Printf("creating job with prompt: %s", req.Prompt)

// CORRECT — truncate
truncated := req.Prompt
if len(truncated) > 100 { truncated = truncated[:100] + "..." }
log.Printf("creating job with prompt: %s", truncated)
```

---

## Rule 9: Rate Limiting Is Per User, Not Per IP

```go
// For authenticated endpoints: rate limit by userID
// IP-based limits are insufficient (shared NAT, VPN bypass)

// Concurrent job check:
count, err := repo.CountActiveJobs(ctx, userID)
if count >= cfg.MaxConcurrentJobsPerUser {
    return c.Status(429).JSON(response.Error("RATE_LIMITED", "Too many concurrent jobs", nil, rid))
}
```

---

## Rule 10: CORS Must Not Use Wildcards in Production

```go
// CORRECT
app.Use(cors.New(cors.Config{
    AllowOrigins: os.Getenv("CORS_ALLOWED_ORIGINS"), // "https://violetstudio.app"
    AllowMethods: "GET,POST,DELETE,OPTIONS",
    AllowHeaders: "Origin, Content-Type, Authorization, X-Request-ID",
}))

// WRONG
AllowOrigins: "*"
```

In development: `http://localhost:3000`
In production: exact production domain

---

## Rule 11: Database Connections Use SSL in Production

```
DATABASE_URL must include: sslmode=require
Not: sslmode=disable (acceptable only in local dev)
```

---

## Rule 12: No Sensitive Data in go.mod or go.sum

These files are committed. Never put:
- Private module paths that reveal internal service names
- Credentials as replace directives

---

## Reviewing for Security

Run the `security-reviewer` agent on any diff that touches:
- Authentication or authorization logic
- File upload handling
- External API calls
- Error response construction
- Rate limiting
- Configuration loading
