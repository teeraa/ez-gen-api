# Backend Security — Agent F Deliverable

> Date: 2026-10-12
> Status: **Proposed** — implementation has not begun
> Scope: Go Fiber backend (`ez-gen-api`) trust boundaries and security rules

---

## 1. Trust Boundaries

```
                        ┌─────────────────┐
  User Browser          │  Next.js App    │  (ez-gen-web — trusted frontend)
  (untrusted)  ────────►│  Route Handler  │
                        └────────┬────────┘
                                 │ Server-side HTTP (API_URL — not exposed to browser)
                        ┌────────▼────────┐
                        │  Go Fiber API   │  (ez-gen-api — this service)
                        │  (ez-gen-api)   │
                        └────────┬────────┘
                                 │ HTTPS + API Key
                        ┌────────▼────────┐
                        │ BytePlus ModelArk│  (external provider — untrusted responses)
                        └─────────────────┘
```

**Key rules:**
- The browser never calls BytePlus directly.
- The browser never sees `MODELARK_API_KEY`.
- The Go backend must treat Next.js Route Handler requests as coming from authenticated
  users — the JWT from the browser session is forwarded to the Go API.
- BytePlus responses must be treated as untrusted external data and validated before
  persisting or forwarding.

---

## 2. Authentication and Authorization

### 2.1 JWT Bearer Tokens

Proposed (not yet implemented):
- Tokens are issued by this backend upon successful authentication.
- `Authorization: Bearer <token>` header required on all protected endpoints.
- Claims: `sub` (user UUID), `exp`, `iat`, `plan`.
- JWT secret loaded from `JWT_SECRET` environment variable.
- Minimum secret entropy: 256 bits (32 random bytes, base64-encoded).

### 2.2 Authorization Model

- Every user-facing endpoint is scoped to the authenticated user's ID.
- A user must only access their own jobs, projects, and uploads.
- Ownership check: `WHERE id = $jobId AND user_id = $userID`.
- If a resource exists but belongs to another user, return **HTTP 404** (not 403).
  Do not confirm the existence of another user's resources.

### 2.3 Internal Routes

Routes under `/api/v1/` are for internal service-to-service calls.
These must require a separate service token (not a user JWT).
Internal routes must not be reachable from the public internet.

### 2.4 OAuth2

The OAuth2 route group (`/oauth/...`) handles third-party authentication flows.
Do not implement OAuth2 changes without a separate security review.

---

## 3. Secret Management

| Secret | Environment Variable | Rotation |
|---|---|---|
| BytePlus API key | `MODELARK_API_KEY` | On compromise or quarterly |
| JWT signing secret | `JWT_SECRET` | On compromise; triggers all session invalids |
| Database password | `DATABASE_URL` | Managed by database team |
| Object storage keys | `STORAGE_ACCESS_KEY`, `STORAGE_SECRET_KEY` | On compromise |

**Rules:**
1. Secrets are loaded once at startup from environment variables.
2. Secrets must never appear in:
   - Log output
   - Error messages returned to clients
   - Git history
   - HTTP response bodies
3. `.env` is in `.gitignore`. Only `.env.example` (with placeholder values) is committed.
4. If a secret appears in a log line, treat it as compromised and rotate immediately.

---

## 4. Input Validation

### 4.1 Request Validation

All request structs use `go-playground/validator/v10` tags:

```go
type CreateJobRequest struct {
    Prompt         string `json:"prompt"     validate:"required,min=1,max=2000"`
    Mode           string `json:"mode"       validate:"required,oneof=text image_video image_image"`
    Model          string `json:"model"      validate:"required"`
    AspectRatio    string `json:"aspectRatio" validate:"required,oneof=16:9 9:16 1:1"`
    Duration       int    `json:"duration"   validate:"required,oneof=5 10"`
    Quality        string `json:"quality"    validate:"required,oneof=standard high"`
    EnhancePrompt  bool   `json:"enhancePrompt"`
    ReferenceImageID *string `json:"referenceImageId" validate:"omitempty,uuid4"`
    IdempotencyKey   *string `json:"idempotencyKey"   validate:"omitempty,uuid4"`
}
```

Validation happens in the **handler** before calling the facade.
Validation errors return HTTP 400 with `code: VALIDATION_ERROR` and a `details` array.

### 4.2 File Upload Validation

For `POST /open/v1/uploads`:
1. Check `Content-Type` header (reject non-multipart).
2. Enforce 8 MB file size limit at the Fiber body limit middleware level.
3. Read the first 512 bytes and validate magic bytes:
   - JPEG: `FF D8 FF`
   - PNG: `89 50 4E 47 0D 0A 1A 0A`
   - WEBP: `52 49 46 46 ... 57 45 42 50`
4. Attempt to decode the image header (width, height) to confirm it is a valid image.
5. Reject files that pass MIME check but fail image decode.

### 4.3 Prompt Content

Prompts are user-supplied text. Rules:
- Length validated (max 2000 characters).
- Do not render prompts as HTML anywhere.
- When logging prompts, truncate to 100 characters to avoid log injection.
- Prompts are passed to BytePlus as-is (the provider handles content moderation).

---

## 5. Rate Limiting

Rate limiting is applied per authenticated user ID (not per IP, to avoid proxy issues).
Fiber's built-in rate limiter or a custom in-memory store.

| Endpoint group | Limit | Window |
|---|---|---|
| POST /open/v1/jobs | 10 requests | 1 minute |
| POST /open/v1/uploads | 20 requests | 1 hour |
| All other | 100 requests | 1 minute |
| Concurrent jobs | 3 per user | Checked at job creation |

For production, use a distributed rate limiter (Redis) to handle multiple instances.

---

## 6. Output Security

### 6.1 Media URLs

Provider output URLs from BytePlus must NOT be stored or forwarded to the frontend.
They may contain credentials or expire unexpectedly.

Required flow:
1. Receive provider output URL.
2. Download media to application object storage immediately.
3. Generate a signed URL from application object storage when the client requests it.
4. Set a reasonable expiry (e.g., 12 hours) on signed URLs.
5. The frontend must handle `mediaExpiresAt` and re-request before expiry.

### 6.2 Error Response Disclosure

Error responses must not expose:
- Stack traces
- SQL error messages
- Internal file paths
- Provider API key or task IDs in error details
- Raw BytePlus error payloads

The `failure_message` stored in the database should be sanitized before display.
The `provider_error` JSONB column stores the raw error for debugging — this column
is NOT included in any API response.

### 6.3 Request ID Correlation

Every request gets a UUID logged on the backend and returned in `X-Request-ID` header
and `requestId` response field. This enables support tracing without exposing internals.

---

## 7. CORS

```go
app.Use(cors.New(cors.Config{
    AllowOrigins: os.Getenv("CORS_ALLOWED_ORIGINS"),
    AllowMethods: "GET,POST,DELETE,OPTIONS",
    AllowHeaders: "Origin, Content-Type, Authorization, X-Request-ID",
    MaxAge:       86400,
}))
```

- Production `CORS_ALLOWED_ORIGINS` must be the exact frontend domain (no wildcards).
- The Go API does not need to serve the browser directly; CORS is for the Next.js
  server-side calls which do not need CORS, but the browser may call via the CDN.

---

## 8. Logging

Structured logging rules:
- Log `requestId`, `method`, `path`, `status`, `latency`, `userID` per request.
- Never log: request bodies, JWT tokens, API keys, prompt full text (truncate to 100 chars).
- Log `jobId` and `providerTaskId` for generation events (not the provider URL).
- Log provider errors at WARN level with the `jobId` but without the raw provider payload.

---

## 9. Dependency Security

```bash
go mod tidy         # Remove unused dependencies
go list -m all      # Review all transitive dependencies
# Periodically run:
govulncheck ./...   # Check for known vulnerabilities (go install golang.org/x/vuln/cmd/govulncheck@latest)
```

Review new dependencies before adding them. Prefer stdlib or well-maintained packages
with active security response.

---

## 10. Unresolved Security Questions

| Question | Risk | Resolution |
|---|---|---|
| Authentication system (OAuth2? JWT? Clerk?) | High | Must be decided before any auth code is written |
| Session token storage strategy | High | Depends on auth system |
| BytePlus webhook signature verification | Medium | Check if BytePlus provides webhook HMAC |
| Object storage access control | High | Signed URLs vs ACL per-object |
| Admin / internal route protection | Medium | Service token strategy TBD |
| Database connection security (TLS) | Medium | Verify `sslmode=require` in production |
