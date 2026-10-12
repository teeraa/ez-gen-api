# Backend Verification Playbook — Agent G Deliverable

> Date: 2026-10-12
> Status: **Proposed** — toolchain verified; test suite not yet written
> Purpose: Reproducible procedures for verifying all backend changes

---

## 1. Pre-merge Gate

Every change must pass all three checks before merging:

```bash
go build ./...       # Compilation check
go vet ./...         # Static analysis
go test -race ./...  # Tests with race detection
```

If any check fails, the change must not be merged. Fix the failure — do not skip checks.

---

## 2. Environment Setup

### 2.1 Local Development Setup

```bash
# 1. Install Go (confirmed: go1.26.1 darwin/amd64)
which go && go version

# 2. Clone and enter repository
cd /Users/mac/Workspace/ez-gen/ez-gen-api

# 3. Resolve dependencies
go get github.com/gofiber/fiber/v2
go get gorm.io/gorm gorm.io/driver/postgres
go get github.com/joho/godotenv
go get github.com/golang-jwt/jwt/v5
go get github.com/google/uuid
go get github.com/go-playground/validator/v10
go mod tidy

# 4. Verify go.sum was created
ls go.sum

# 5. Configure environment
cp .env.example .env
# Edit .env — set DATABASE_URL, JWT_SECRET

# 6. Start local PostgreSQL (Docker example)
docker run -d --name violetstudio-db \
  -e POSTGRES_DB=violetstudio \
  -e POSTGRES_USER=violetstudio \
  -e POSTGRES_PASSWORD=localdev \
  -p 5432:5432 \
  postgres:15

# 7. Build and run
go run .
# Should print: Fiber listening on :8080

# 8. Smoke test
curl http://localhost:8080/health
# Expected: {"status":"ok"}
```

### 2.2 Environment Variable Validation

```bash
# Verify all required variables are set
go run ./cmd/check-env/main.go  # (to be implemented)
# Or manually:
[ -z "$DATABASE_URL" ] && echo "MISSING: DATABASE_URL"
[ -z "$JWT_SECRET" ] && echo "MISSING: JWT_SECRET"
[ -z "$MODELARK_API_KEY" ] && echo "MISSING: MODELARK_API_KEY"
```

---

## 3. Build Verification

```bash
# Compile all packages
go build ./...

# Build the main binary
go build -o ./bin/ez-gen-api .

# Verify the binary runs
./bin/ez-gen-api &
curl http://localhost:8080/health
kill %1
```

---

## 4. Static Analysis

```bash
# Built-in Go vet (required)
go vet ./...

# Format check (required)
gofmt -l .
# If any files are printed, run: gofmt -w .

# Optional (install separately):
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...

go install honnef.co/go/tools/cmd/staticcheck@latest
staticcheck ./...
```

---

## 5. Unit Tests

### 5.1 Running Tests

```bash
# All tests
go test ./...

# With race detection (required before merging)
go test -race ./...

# Verbose output
go test -v ./...

# Specific package
go test -v ./apps/handlers/handler_video_generation/...

# Coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### 5.2 Test File Conventions

- Test files live alongside their source: `foo.go` → `foo_test.go`
- Shared test helpers live in `apps/tests/`
- Table-driven tests are preferred for request validation
- Use `t.Parallel()` in unit tests

### 5.3 Required Test Coverage Areas

| Area | Test Type | External Required | Notes |
|---|---|---|---|
| Request struct validation | Unit | No | Table-driven, every field |
| Handler — success path | Unit | No | Mock facade |
| Handler — validation error | Unit | No | Confirm 400 + details |
| Handler — unauthorized | Unit | No | Confirm 404 (not 403) |
| Handler — facade error mapping | Unit | No | ErrNotFound → 404, etc. |
| Facade — state machine transitions | Unit | No | Mock repo + service |
| Facade — idempotency key behavior | Unit | No | Mock repo |
| Repository — CRUD | Integration | Yes (DB) | PostgreSQL required |
| Repository — concurrent update safety | Integration | Yes (DB) | SELECT FOR UPDATE |
| ModelArk client — create task | Unit | No | Mock HTTP transport |
| ModelArk client — get task status | Unit | No | Mock HTTP transport |
| ModelArk mapper — status mapping | Unit | No | All provider status values |
| ModelArk client — error response handling | Unit | No | All error codes |
| File upload — magic bytes | Unit | No | Each accepted type + rejection |
| File upload — size limit | Unit | No | At limit and over limit |

### 5.4 Mock HTTP Transport for ModelArk

Tests for `apps/services/modelark/` must use a mock HTTP transport:

```go
// apps/tests/mock_http.go (to be implemented)
type MockTransport struct {
    Responses map[string]*http.Response
}

func (m *MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    // Match on URL path, return configured response
}
```

Never call real BytePlus endpoints in tests.
Never require `MODELARK_API_KEY` to run tests.

### 5.5 Database Tests

Integration tests that require a live database must:
1. Use a test-only database (separate from development/production).
2. Be isolated in a `_test.go` file with a build tag or skip condition:
   ```go
   func TestMain(m *testing.M) {
       if os.Getenv("INTEGRATION_TEST_DB") == "" {
           os.Exit(0) // Skip if no DB configured
       }
       // setup + teardown
   }
   ```
3. Run each test in a transaction, rolled back at teardown.
4. Never modify production data.

---

## 6. Integration Tests

Integration tests require a live PostgreSQL instance. They:
- Verify the full request → handler → facade → repository → DB path.
- Test actual SQL queries, indexes, and constraints.
- Run the migration files against a test database.

```bash
INTEGRATION_TEST_DB="postgres://violetstudio:localdev@localhost:5432/violetstudio_test" \
  go test -tags integration ./...
```

---

## 7. Race Detection

The status poller is concurrent. Race detection is mandatory:

```bash
go test -race ./apps/crons/...
go test -race ./apps/facades/...
```

Any data race is a critical bug. Do not suppress with `//nolint:` without coordinator approval.

---

## 8. Docker Build Verification

```bash
docker build -t ez-gen-api:local .
docker run --rm -p 8080:8080 --env-file .env ez-gen-api:local
curl http://localhost:8080/health
```

---

## 9. API Contract Validation

Before merging any endpoint change:
1. Compare the endpoint's response shape against `docs/engineering/video-generation-api-contract.md`.
2. Compare against `../ez-gen-web/docs/engineering/api-contract.md`.
3. If the response shape diverges, update both contract files and notify the frontend team.

Manual spot-check:
```bash
# Start server
go run . &

# Create job (requires auth token — TBD)
curl -X POST http://localhost:8080/open/v1/jobs \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"prompt":"test","mode":"text","model":"violet-motion-2","aspectRatio":"16:9","duration":5,"quality":"standard","enhancePrompt":false}'

# Expect: 201 Created with jobId, status=queued
```

---

## 10. Migration Review

Before applying any migration:
1. Review the SQL file for destructive operations.
2. Verify it is the correct migration version.
3. Apply to a staging database first.
4. Verify with `\d tablename` that the schema matches the proposal.

```bash
# Review migrations (no destructive commands here)
ls migrations/
cat migrations/001_create_users.sql
```

Never run migrations against a production database without a backup and explicit authorization.

---

## 11. Pre-implementation Checklist

Before any implementation agent begins:
- [ ] `go mod tidy` run, `go.sum` committed
- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes with no warnings
- [ ] Local PostgreSQL running
- [ ] `.env` configured
- [ ] `curl http://localhost:8080/health` → `{"status":"ok"}`
- [ ] `docs/engineering/modelark-integration-research.md` verified against live docs
- [ ] `docs/engineering/video-generation-api-contract.md` reconciled with frontend

---

## 12. Known Gaps

| Gap | Impact | When to Address |
|---|---|---|
| No test suite yet | Cannot verify behavior | Before first feature implementation |
| No `check-env` command | Setup errors not caught early | During first feature setup |
| No migration runner script | Schema changes risky | Before first DB migration |
| No CI/CD pipeline | Checks not automatic | Before production deployment |
| No coverage threshold enforced | Tests can be incomplete | Before first release |
