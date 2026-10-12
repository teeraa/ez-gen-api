# Skill: verify-backend

Run the full backend verification suite and report results. Use after any code change before marking a task complete.

## When to Use

- After implementing a feature (pre-merge verification)
- After resolving a merge conflict
- After updating dependencies
- When setting up a new development environment
- As a sanity check before starting a new task

---

## Step 1: Environment Check

```bash
# Go version
go version
# Expected: go1.26.1 or higher

# Module
head -1 go.mod
# Expected: module github.com/ez-gen/ez-gen-api

# Dependencies resolved
ls go.sum
# If missing: go mod tidy

# Environment variables
[ -z "$DATABASE_URL" ] && echo "MISSING: DATABASE_URL" || echo "OK: DATABASE_URL"
[ -z "$ARK_API_KEY"  ] && echo "MISSING: ARK_API_KEY (required for generation features)" || echo "OK: ARK_API_KEY"
[ -z "$JWT_SECRET"   ] && echo "MISSING: JWT_SECRET" || echo "OK: JWT_SECRET"
```

---

## Step 2: Build

```bash
go build ./...
```

Expected: no output, exit code 0.

If it fails:
- Missing import → run `go get <package>` then `go mod tidy`
- Type error → find the file and fix the type
- Never suppress a compile error

---

## Step 3: Static Analysis

```bash
go vet ./...
```

Expected: no output.

Common issues:
- `printf` format mismatch → fix the format string
- Unreachable code → remove it
- Copying a mutex by value → use pointer receiver

Optional (if installed):
```bash
staticcheck ./...
gofmt -l .   # prints files that need formatting
```

---

## Step 4: Unit Tests

```bash
go test -race ./...
```

Expected: `ok` for all packages, `[no test files]` for packages without tests.

If a test fails:
- Read the failure message completely
- Do not use `-run` to skip failing tests — fix them

Race condition detected:
- A race is a critical bug — fix before proceeding
- Do not suppress with `//nolint`

---

## Step 5: Integration Tests (if DB available)

```bash
INTEGRATION_TEST_DB="postgres://violetstudio:localdev@localhost:5432/violetstudio_test" \
  go test -tags integration -race ./...
```

Expected: same as unit tests.

If DB is not available, skip this step and note it in the report.

---

## Step 6: Health Check

```bash
# Start the server in background
go run . &
SERVER_PID=$!
sleep 1

# Health endpoint
curl -s http://localhost:8080/health
# Expected: {"status":"ok"}

# Kill server
kill $SERVER_PID
```

---

## Step 7: Format Check

```bash
UNFORMATTED=$(gofmt -l .)
if [ -n "$UNFORMATTED" ]; then
    echo "Unformatted files:"
    echo "$UNFORMATTED"
    echo "Run: gofmt -w ."
else
    echo "All files formatted"
fi
```

---

## Step 8: API Contract Spot Check (if endpoints changed)

For each modified endpoint:

```bash
# Start server
go run . &

# Test the endpoint shape (replace with actual auth token when available)
curl -s -X POST http://localhost:8080/open/v1/jobs \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <token>" \
  -d '{"prompt":"test","mode":"text","model":"violet-motion-2","aspectRatio":"16:9","duration":5,"quality":"standard","enhancePrompt":false}'

# Verify response has: data.jobId, data.status, data.createdAt, requestId
# Verify HTTP status: 201

kill %1
```

Spot-check response fields against `docs/engineering/video-generation-api-contract.md`.

---

## Verification Report Template

```
## Verification Report — <date> — <feature or task name>

### Environment
- Go: <version>
- Module: github.com/ez-gen/ez-gen-api
- DATABASE_URL: set / not set
- ARK_API_KEY: set / not set

### Results

| Check | Result | Notes |
|---|---|---|
| go build ./... | PASS / FAIL | |
| go vet ./... | PASS / FAIL | |
| go test -race ./... | PASS / FAIL | N packages, N tests |
| Integration tests | PASS / SKIP / FAIL | Reason if skipped |
| Health check | PASS / FAIL | |
| Format check | PASS / FAIL | |
| API contract spot check | PASS / SKIP / FAIL | |

### Failures
<list any failures with file:line and description>

### Blocked By
<list any checks that could not run and why>

### Ready for Review: YES / NO
```

---

## When Checks Fail

| Failure | Do This |
|---|---|
| `go build` fails | Fix compile errors before anything else |
| `go vet` warning | Fix — vet warnings indicate real bugs |
| Test fails | Fix the test or fix the code — do not skip |
| Race condition | Fix immediately — critical |
| Health check fails | Check PORT, check .env, check main.go wiring |
| Format check fails | Run `gofmt -w .` |
