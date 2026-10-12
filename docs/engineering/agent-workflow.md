# Agent Workflow — Agent H Deliverable

> Date: 2026-10-12
> Status: **Active** — this workflow applies immediately
> Maintainer: Agent A (Coordinator)

---

## 1. Agent Chain Overview

Implementation follows a strict chain. Each step must complete before the next begins.
Research steps (2, 3, 4, 5, 6) may be parallelized. Implementation steps are sequential.

```
1. Coordinator         Establish scope and acceptance criteria
2. Reconnaissance      Gather repository and provider context          ─┐
3. Architecture        Propose component design                         ├─ parallelizable
4. ModelArk Research   Verify provider API behavior                     │
5. Database            Validate data model                              │
6. Security            Review trust boundaries                         ─┘
7. Implementation      Execute bounded task (feature code)
8. Test Agent          Verify behavior (unit + integration)
9. Review Agent        Independent diff and test evidence review
10. Coordinator        Accept or reject based on evidence
```

---

## 2. Implementation Task Template

Every implementation task must be scoped with this template before an agent begins.
The coordinator fills this template and posts it to `AGENTS.md` or a task-specific file.

```markdown
## Task: <name>

### Assigned Agent
<agent description>

### Scope
<what this task does — be specific>

### Files Allowed to Change
- `apps/models/video_generation_job.go` — add GORM struct
- (list every file explicitly)

### Files Prohibited from Changing
- `CLAUDE.md`, `AGENTS.md` — coordinator-only
- `main.go` unless adding a single dependency wire
- Any file outside the listed allowed paths

### Dependencies (must be complete first)
- `docs/engineering/video-data-model-proposal.md` — schema confirmed
- `docs/engineering/modelark-integration-research.md` — provider status values confirmed

### Acceptance Criteria
- [ ] `go build ./...` passes
- [ ] `go vet ./...` passes
- [ ] `go test -race ./apps/...` passes
- [ ] Handler returns HTTP 201 with correct schema on valid input
- [ ] Handler returns HTTP 400 with `details` on invalid input
- [ ] No hard-coded credentials
- [ ] No provider-specific fields in response DTOs

### Required Tests
- `apps/handlers/handler_video_generation/create_job_test.go`
  - Table-driven: valid input, missing prompt, invalid mode, missing referenceImageId
  - Uses mock facade — no database required
- `apps/facades/facade_video_generation/create_job_test.go`
  - Idempotency key: duplicate key returns existing job
  - Uses mock repository and mock service

### Prohibited Side Effects
- Must not modify existing handler behavior
- Must not run AutoMigrate
- Must not make live BytePlus API calls

### Completion Evidence
- `go test -race ./apps/...` output (all PASS, no race conditions)
- `go vet ./...` output (clean)
- `curl` smoke test against running server showing correct response shape
```

---

## 3. File Modification Protocol

1. **Read before editing.** Always read the current file state before making changes.
2. **One agent per file.** Check `AGENTS.md` file ownership before editing.
3. **Announce intent.** If a file is not assigned to you and you need to change it,
   coordinate with Agent A first.
4. **No concurrent edits.** Agents working in parallel must not edit the same file.
   If parallelization would cause a conflict, run those tasks sequentially.

---

## 4. Instruction Hierarchy

When instructions conflict, this hierarchy resolves them:

1. `CLAUDE.md` — project-level rules (highest priority)
2. `AGENTS.md` — agent roster and handoff rules
3. `docs/engineering/<document>.md` — domain-specific rules
4. General Go community conventions (lowest priority)

If you find a conflict between levels, report it to Agent A before proceeding.
Do not silently follow one source over another.

---

## 5. Environment Validation Sequence

Before starting any work session:

```bash
# 1. Confirm Go version
go version
# Expected: go1.26.1 or higher

# 2. Confirm module
head -1 go.mod
# Expected: module github.com/ez-gen/ez-gen-api

# 3. Confirm dependencies resolved
ls go.sum
# If missing: go mod tidy

# 4. Build check
go build ./...
# Must pass

# 5. Vet check
go vet ./...
# Must pass with no output

# 6. Health check
go run . &
curl http://localhost:8080/health
kill %1
# Expected: {"status":"ok"}
```

---

## 6. Repeatable Verification Commands

| Command | Purpose | Required Before |
|---|---|---|
| `go build ./...` | Compilation | Any commit |
| `go vet ./...` | Static analysis | Any commit |
| `go test -race ./...` | Tests + race detection | Any merge |
| `gofmt -l .` | Format check | Any commit |
| `govulncheck ./...` | Vulnerability scan | Any release |
| `docker build .` | Docker correctness | Any infra change |
| `go mod tidy` | Clean dependencies | After any `go get` |

---

## 7. Phase Transition Criteria

### Preparation Phase → Implementation Phase

The coordinator declares the preparation phase complete when ALL of the following
are confirmed (not just documented):

- [ ] `go build ./...` passes (after `go mod tidy`)
- [ ] `go vet ./...` passes
- [ ] `docs/engineering/modelark-integration-research.md` is complete with verified
      provider status values, model IDs, and output URL lifetime
- [ ] `docs/engineering/video-generation-api-contract.md` is reconciled with
      `../ez-gen-web/docs/engineering/api-contract.md`
- [ ] `docs/engineering/video-data-model-proposal.md` is reviewed against the
      authentication system design
- [ ] `docs/engineering/backend-security.md` authentication section is filled in
      (mechanism chosen)
- [ ] `git init` and initial commit done
- [ ] Local PostgreSQL provisioned and reachable

### Implementation Phase → Test Phase

Each task must have:
- [ ] All acceptance criteria in the task template checked
- [ ] Test files written and passing
- [ ] No regressions in existing tests

### Test Phase → Review Phase

- [ ] `go test -race ./...` passes with full output
- [ ] Coverage report reviewed
- [ ] Smoke test against running server passes

### Review Phase → Coordinator Acceptance

- [ ] Independent review agent inspected the diff
- [ ] Reviewer found no security issues
- [ ] Reviewer confirmed test coverage is adequate
- [ ] Coordinator approved

---

## 8. Background Worker (Cron) Workflow

The status poller (background goroutine) has special requirements:

1. It must not start until the first job enters `submitted` state.
2. It must gracefully shut down on `os.Signal` (SIGTERM, SIGINT).
3. It must not crash the server if the provider is unreachable.
4. It must not create duplicate provider calls for the same job.
5. Tests for the poller must use mock time and mock provider calls.

Implementation agent must get coordinator approval on the poller design
before writing any goroutine code.

---

## 9. Cross-Repository Coordination

The backend and frontend share the API contract. When an endpoint changes:

1. Update `docs/engineering/video-generation-api-contract.md` (this repo).
2. Notify the frontend team to update `../ez-gen-web/docs/engineering/api-contract.md`.
3. Both updates should land in the same coordination window before either
   side's implementation begins.

The two contract files must always agree. The backend version is authoritative for
field names, HTTP status codes, and error code strings.

---

## 10. Blockers and Escalation

If an agent is blocked (missing information, conflicting instructions, dangerous action required):

1. Do NOT proceed with a guess.
2. Document the blocker in `docs/engineering/baseline-report.md` under "Blockers".
3. State what is known, what is unknown, and what safe default was chosen.
4. Notify Agent A (Coordinator).

Examples of blockers requiring escalation:
- BytePlus model IDs not found in documentation
- Authentication system choice unresolved
- Conflicting instructions between `CLAUDE.md` and this document
- A task requires modifying a file owned by another agent
