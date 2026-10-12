# VIOLET STUDIO — Backend Agent Team Responsibilities

> This file is maintained by Agent A (Coordinator).
> See `CLAUDE.md` for development rules and environment setup.

## Agent Roster

| Agent | Role | Primary Files |
|---|---|---|
| **A — Coordinator** | Architecture decisions, conflict resolution, final sign-off | `CLAUDE.md`, `AGENTS.md`, `docs/engineering/` |
| **B — Repository Reconnaissance** | Inspect codebase, record baseline | `docs/engineering/repository-map.md`, `docs/engineering/baseline-report.md` |
| **C — Go Architecture Specialist** | Layered architecture mapping, component placement rules | `docs/engineering/backend-architecture.md` |
| **D — ModelArk Integration Specialist** | BytePlus API research and integration design | `docs/engineering/modelark-integration-research.md` |
| **E — Database Specialist** | Data model design, migration plan | `docs/engineering/video-data-model-proposal.md` |
| **F — Security Specialist** | Trust boundaries, auth patterns, secret handling | `docs/engineering/backend-security.md`, `docs/engineering/video-generation-api-contract.md` |
| **G — Quality Specialist** | Test strategy, verification procedures | `docs/engineering/backend-verification-playbook.md` |
| **H — Developer Experience Specialist** | Agent workflow, environment procedures | `docs/engineering/agent-workflow.md` |

## Current Phase: ENVIRONMENT PREPARATION

**What is complete:**
- Go module initialized (`github.com/ez-gen/ez-gen-api`, Go 1.26.1)
- Directory skeleton created
- `CLAUDE.md`, `AGENTS.md`, `.env.example`, `Dockerfile`, `.gitignore` created
- All 9 engineering documents produced in `docs/engineering/`
- Frontend `ez-gen-web` preparation phase already complete

**What is NOT yet done:**
- No product features implemented
- No real BytePlus API calls connected
- No authentication middleware implemented
- No database schema created
- No GORM models created
- Dependencies not yet resolved (`go mod tidy` not run)

## Handoff Rules

1. **One owner per file.** Only the assigned agent edits its deliverable.
   Coordinate through Agent A if two agents need the same file.
2. **No concurrent edits.** Do not edit the same file simultaneously.
3. **Verify before marking done.** Code changes require `go build && go vet && go test`.
   Documentation changes require human review and checklist entry.
4. **Document blockers immediately.** If a task cannot be completed, add the
   blocker to `docs/engineering/baseline-report.md` section "Blockers".

## Implementation Phase Prerequisites (not yet started)

Before any implementation agent begins:
- [ ] `go mod tidy` run and `go.sum` committed
- [ ] Database schema reviewed and migration files created in `migrations/`
- [ ] `docs/engineering/video-generation-api-contract.md` reconciled with
      `../ez-gen-web/docs/engineering/api-contract.md`
- [ ] `docs/engineering/modelark-integration-research.md` verified against live docs
- [ ] `.env` configured locally with real test credentials
- [ ] `go build ./...` passes

## Implementation Agent Scope Template

Each implementation task must define:
```
## Task: <name>

### Scope
<what this task does>

### Files Allowed to Change
- <path> — <reason>

### Files Prohibited from Changing
- <path> — <reason>

### Dependencies
- <prerequisite task or document>

### Acceptance Criteria
- [ ] <specific, verifiable condition>

### Required Tests
- <test file and what it must verify>

### Prohibited Side Effects
- Must not change existing behavior of <X>
- Must not modify <Y>

### Completion Evidence
- `go test ./apps/...` passes
- No new `go vet` warnings
- PR diff reviewed by independent review agent
```

## File Ownership Map

| Path | Owned by | Notes |
|---|---|---|
| `CLAUDE.md` | Agent A | Do not edit unless resolving conflict |
| `AGENTS.md` | Agent A | This file |
| `.env.example` | Agent A/F | Security agent approves new env vars |
| `go.mod` / `go.sum` | Agent A | Dependency changes require coordinator approval |
| `main.go` | Agent C | Root wiring only |
| `apps/models/` | Agent E | Schema changes must go through migration |
| `apps/repositories/` | Implementation agents | One agent per repository package |
| `apps/facades/` | Implementation agents | One agent per facade package |
| `apps/handlers/` | Implementation agents | One agent per handler package |
| `apps/services/modelark/` | Agent D | ModelArk client code |
| `apps/dto/` | Implementation agents | Match API contract |
| `apps/requests/` | Implementation agents | Match API contract |
| `apps/db/` | Agent E | Database connection only |
| `migrations/` | Agent E | Migration files only |
| `docs/engineering/` | Respective agents | See Agent Roster above |
