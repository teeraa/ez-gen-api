# Skill: review-go

Review a Go code change for correctness, style, security, and architecture compliance.

## When to Use

After any implementation is complete and before coordinator acceptance. Also use for:
- Reviewing a PR diff
- Spot-checking a specific file
- Pre-merge review after the implementation agent is done

---

## Review Scope Levels

Pass a scope argument when invoking:
- `/review-go handler` — review handler + request + DTO files only
- `/review-go facade` — review facade + service interaction
- `/review-go full` — review the entire diff end-to-end

---

## Phase 1: Architecture Review

```
[ ] Does the handler contain business logic? (move to facade if yes)
[ ] Does the handler import a repository package? (must only import facade interface)
[ ] Does the facade return a raw GORM model? (must return DTO)
[ ] Does the facade import a handler package? (never allowed)
[ ] Do any BytePlus SDK types appear outside apps/services/modelark/? (violation)
[ ] Is the new code wired via constructor injection? (no global variables)
```

---

## Phase 2: Go Conventions

```
[ ] Error wrapping: errors wrap context with fmt.Errorf("action: %w", err)
[ ] Sentinel errors used: errors.Is(err, ErrNotFound) not string comparison
[ ] No naked returns (return without values in named-return functions)
[ ] No unused variables or imports (will fail go vet)
[ ] Context propagated: ctx passed to all DB and HTTP calls
[ ] defer used correctly: defer rows.Close() after error check, not before
[ ] No goroutine leak: every goroutine has a termination path (ctx.Done)
[ ] Interfaces defined in the consuming package, not the implementing package
```

---

## Phase 3: Security Review

Run the `security-reviewer` agent for a full security review. Quick checks here:

```
[ ] ARK_API_KEY never appears in log statements
[ ] User input never formatted into SQL directly (use GORM parameterized queries)
[ ] userID read from c.Locals("userID"), never from body/query
[ ] 404 returned for unauthorized resource access (not 403)
[ ] Error responses use the standard envelope (no raw error strings)
[ ] No stack traces in HTTP responses
[ ] File uploads: magic bytes validated, not just Content-Type header
```

---

## Phase 4: Test Coverage

```
[ ] Handler test covers: success, validation error, ErrNotFound→404, ErrConflict→409, 500
[ ] Facade test covers: success, idempotency, ownership check, error propagation
[ ] Mapper test covers: all 5 provider status values (if touching modelark)
[ ] No test requires ARK_API_KEY or a live database (unless tagged integration)
[ ] Table-driven tests for validation logic
```

---

## Phase 5: Code Quality

```
[ ] No comments describing WHAT the code does (only WHY for non-obvious decisions)
[ ] No multi-line comment blocks
[ ] No TODO/FIXME left in merged code (create a follow-up task instead)
[ ] Function length: prefer under 40 lines; flag anything over 80 for discussion
[ ] No repeated code that should be extracted (but don't abstract prematurely)
[ ] All exported symbols have meaningful names
```

---

## Phase 6: Response Shape

```
[ ] Success response: {"data": {...}, "requestId": "..."}
[ ] Error response: {"code": "...", "message": "...", "requestId": "..."}
[ ] HTTP status codes match docs/engineering/video-generation-api-contract.md
[ ] Field names in DTOs match the contract exactly (camelCase)
[ ] Timestamps formatted as ISO 8601 strings
[ ] Nullable fields present but null (not omitted) when not set
```

---

## Reporting Format

```
## Review: <scope> — <file or PR>

### PASS ✅ / FAIL ❌ / NEEDS DISCUSSION ⚠️

### Findings

SEVERITY | FILE:LINE | ISSUE | REQUIRED FIX
---------|-----------|-------|-------------
HIGH     | apps/handlers/handler_job/handler.go:42 | Handler calls repo directly | Move through facade
MEDIUM   | apps/facades/facade_job/facade.go:88 | Error not wrapped with context | Use fmt.Errorf("create job: %w", err)
LOW      | apps/dto/dto_job/response.go:15 | Field name `created_at` should be `createdAt` | Rename to match contract

### Blocking Issues (must fix before merge)
- ...

### Non-blocking Notes
- ...
```
