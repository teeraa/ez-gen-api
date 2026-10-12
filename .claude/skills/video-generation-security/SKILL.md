# Skill: video-generation-security

Security review and implementation checklist specific to the video generation
feature: prompt validation, reference image validation, provider credential
handling, output URL security, and provider error sanitization.

## Scope Boundary

This skill covers security concerns **specific to video generation**.
General project-wide security rules (JWT auth, CORS, rate limiting, response
envelopes) live in `.claude/rules/security.md`. Read that file first.

**This skill adds to those rules — it does not replace them.**

---

## When to Invoke

- Before implementing any generation endpoint (run as pre-implementation check)
- When reviewing a PR that touches prompt handling, file uploads, or provider calls
- When adding a new generation parameter or reference asset type
- After a BytePlus API change that affects how responses are handled
- When `security-reviewer` agent reports a finding in generation code

---

## Checklist: Prompt Security

```
[ ] Prompt length validated: min=1, max=2000 (or model-specific MaxPromptLength)
[ ] Prompt logged at DEBUG only, truncated to 150 chars
[ ] Compiled DNA prompt (style + character + user + scene) also length-checked
[ ] optimize_prompt: false used by default when sending compiled DNA prompts
    (avoids BytePlus rewriting a carefully constructed identity anchor)
[ ] Prompt is passed to BytePlus as-is — no HTML escaping (would corrupt natural language)
[ ] SceneInstructions length validated: max=500 chars
[ ] CharacterDNA.PromptAnchor length validated: max=300 chars per character
[ ] StyleDNA.PromptPrefix length validated: max=300 chars
```

**Why `optimize_prompt: false` for DNA prompts:**
BytePlus's prompt optimizer may rewrite or simplify prompts, discarding
character anchors and style constraints that were carefully composed.
Only enable `optimize_prompt` when the user explicitly enables `enhancePrompt`
AND no DNA layers are active.

---

## Checklist: Reference Image Security

```
[ ] File size limit enforced at Fiber middleware level (not just application code)
    app.Use(limiter.New(...)) — set Body limit to 8 MB
[ ] Magic bytes validated (not just Content-Type header):
    - JPEG: bytes[0:3] == FF D8 FF
    - PNG:  bytes[0:8] == 89 50 4E 47 0D 0A 1A 0A
    - WEBP: bytes[0:4] == RIFF && bytes[8:12] == WEBP
[ ] Image decode attempted to confirm valid image structure
[ ] MIME type from Content-Type header compared against detected magic bytes
[ ] upload.user_id == authenticated userID (verify ownership before using in ContentItem)
[ ] upload.expires_at > NOW() (verify not expired before using in ContentItem)
[ ] Reference image URL passed to BytePlus is a signed URL from app storage,
    NOT the original upload URL or a user-supplied URL
```

**Why use signed URLs from app storage (not user-supplied URLs):**
BytePlus fetches the URL server-side. Allowing user-supplied URLs would enable
SSRF — an attacker could supply an internal service URL.

```go
// WRONG — user-supplied URL
content.ImageURL = &URLRef{URL: req.ReferenceImageURL}

// CORRECT — signed URL from app storage
signedURL, err := f.storage.SignedURL(ctx, upload.StorageKey, 30*time.Minute)
content.ImageURL = &URLRef{URL: signedURL}
```

---

## Checklist: Provider Credential Security

```
[ ] ARK_API_KEY read only via os.Getenv("ARK_API_KEY") — never from request, never hardcoded
[ ] ARK_API_KEY never appears in any log statement (INFO or higher)
[ ] ARK_API_KEY never appears in error messages returned to clients
[ ] SDK client initialized once at startup, not per-request
[ ] Test files do not contain real API keys
[ ] ARK_API_KEY presence checked at startup:
    if os.Getenv("ARK_API_KEY") == "" {
        log.Fatal("ARK_API_KEY is required")
    }
```

---

## Checklist: Output URL Security

```
[ ] response.content.video_url from BytePlus is NEVER:
    - Stored in the database
    - Returned in an API response
    - Logged at INFO or higher
[ ] The URL is downloaded and stored in application object storage immediately
[ ] Only the object storage KEY is stored in output_media_key column
[ ] Signed URLs are generated on demand with a short TTL (12 hours recommended)
[ ] Signed URL expiry is included in the API response as mediaExpiresAt
[ ] Client is expected to re-fetch the job to get a fresh signed URL after expiry
```

---

## Checklist: Provider Error Sanitization

```
[ ] Raw provider error payload (response.error) is stored in provider_error JSONB column
    for debugging, NOT in failure_message
[ ] failure_message contains only a safe, sanitized description:
    - Remove any URLs
    - Remove any model-internal details
    - Keep only the error code and a generic human-readable description
[ ] provider_error column is NEVER included in any DTO or API response
[ ] Content policy errors (OutputVideoSensitiveContentDetected) are logged at WARN,
    not ERROR — they are expected events, not system failures
```

Sanitization example:
```go
func sanitizeFailureMessage(code, rawMessage string) string {
    switch code {
    case "OutputVideoSensitiveContentDetected":
        return "Generation blocked by content policy"
    case "InvalidParameter.TaskTypeConstraint":
        return "Invalid generation parameters"
    default:
        return "Generation failed"
    }
    // Do NOT include rawMessage — it may contain internal provider details
}
```

---

## Checklist: Concurrent Job Limits

```
[ ] Before creating a new job, check concurrent active jobs for this user:
    SELECT COUNT(*) FROM video_generation_jobs
    WHERE user_id = $1 AND status IN ('queued','submitted','processing')
    AND deleted_at IS NULL
[ ] If count >= MAX_CONCURRENT_JOBS_PER_USER: return 429 RATE_LIMITED
[ ] MAX_CONCURRENT_JOBS_PER_USER read from env var (default: 3)
[ ] Check is inside a database transaction with FOR UPDATE on the count query
    to prevent race conditions on simultaneous requests
```

---

## Checklist: DNA Asset Authorization

```
[ ] CharacterDNA.ProjectID must belong to the requesting user's project
[ ] StyleDNA.ProjectID must belong to the requesting user's project
[ ] Reference image upload.user_id must match authenticated userID
[ ] Previous job used for scene continuity: job.user_id must match authenticated userID
[ ] No cross-user DNA sharing without explicit sharing mechanism (not yet implemented)
```

---

## Security Review Output Format

When running as a security review (invoked by `security-reviewer` agent):

```
## Generation Security Review — <date> — <scope>

### Critical (blocks merge)
- ...

### High (should fix before merge)
- ...

### Medium (fix in follow-up)
- ...

### Verified Clean
- Prompt length: ✓
- Magic bytes: ✓
- ARK_API_KEY handling: ✓
- Output URL not stored: ✓
- (etc.)
```

---

## Acceptance Criteria

- [ ] All checklist items above are verified or documented as not-yet-applicable
- [ ] `security-reviewer` agent review passes with no Critical or High findings
- [ ] No `ARK_API_KEY` appears in test files or log output
- [ ] Reference image ContentItem uses signed URL from app storage, not raw upload URL
- [ ] `provider_error` field is absent from all DTO structs

---

## Related Skills

- `reference-assets-and-storage` — upload validation and signed URL generation
- `modelark-provider-integration` — error classification and provider error handling
- `modelark-integration-testing` — validates security properties via tests
- `creative-dna-prompt-compiler` — prompt length and `optimize_prompt` handling
