# VIOLET STUDIO — Go Backend Claude Code Instructions

## Project Overview

VIOLET STUDIO is a premium AI video generation platform. This repository (`ez-gen-api`)
is the Go Fiber backend API. The Next.js frontend is in the companion repository (`ez-gen-web`).
AI generation is performed through BytePlus ModelArk — never called directly from the frontend.

## Stack

| Technology | Version | Notes |
|---|---|---|
| Go | 1.26.1 | Minimum version |
| Fiber | v2.52+ | HTTP framework |
| GORM | v1.31+ | ORM |
| PostgreSQL | 15+ | Primary database |
| godotenv | v1.5+ | Environment loading |
| ark-runtime-go | v0.11+ | Official BytePlus ModelArk SDK |

## Module Path

```
github.com/ez-gen/ez-gen-api
```

Update this path if the GitHub repository organization changes.

## Engineering Documentation

All architecture decisions are in `docs/engineering/`:

| File | Owner | Contents |
|---|---|---|
| `repository-map.md` | Agent B | File structure, scripts, interactions |
| `baseline-report.md` | Agent B | Build, test, toolchain baseline |
| `backend-architecture.md` | Agent C | Layered arch, component rules |
| `modelark-integration-research.md` | Agent D | BytePlus API behavior |
| `video-data-model-proposal.md` | Agent E | DB schema proposals |
| `video-generation-api-contract.md` | Agent F (backend) | API contract (reconciled with frontend) |
| `backend-security.md` | Agent F | Trust boundaries, secret handling |
| `backend-verification-playbook.md` | Agent G | How to verify all changes |
| `agent-workflow.md` | Agent H | Agent chain and handoff rules |

Read the relevant document before implementing any feature in that area.
Reconcile `video-generation-api-contract.md` with `../ez-gen-web/docs/engineering/api-contract.md`
before writing any frontend-facing endpoint.

## Architecture

### Layer Order (dependency direction: inward only)

```
Handler → Facade → Repository → Model
            ↓
         Service (external providers, e.g., ModelArk)
```

- **Models** (`apps/models/`) — GORM structs. No business logic.
- **Repositories** (`apps/repositories/`) — database operations only.
- **Services** (`apps/services/`) — external integrations (BytePlus, storage, email).
- **Facades** (`apps/facades/`) — orchestrate repositories + services. Business logic lives here.
- **Handlers** (`apps/handlers/`) — HTTP: parse request, call facade, format response.
- **DTOs** (`apps/dto/`) — response shapes returned to callers.
- **Requests** (`apps/requests/`) — validated input shapes from callers.
- **Crons** (`apps/crons/`) — scheduled background jobs (status sync, cleanup).

### Route Groups

| Group | Prefix | Auth |
|---|---|---|
| Internal API | `/api/v1/...` | Internal service token |
| Open API | `/open/v1/...` | User JWT bearer token |
| OAuth2 | `/oauth/...` | OAuth2 flow |

The video-generation endpoints belong in the **Open API** group.

## Development Commands

```bash
go build ./...            # Compile
go test ./...             # All tests
go test -race ./...       # Race detection
go vet ./...              # Static analysis
gofmt -l .                # Format check
go run .                  # Start dev server (reads .env)
```

Pre-merge gate: `go build ./... && go vet ./... && go test -race ./...`

## Baseline Verification (2026-10-12)

- Repository: **empty** — project initialization phase
- `go build ./...` → requires dependencies to be added (`go get`)
- `go test ./...` → no tests yet
- Go toolchain: `go1.26.1 darwin/amd64`

Any new failure after changes is a regression and must be fixed before merging.

## Critical Rules

### Security
1. BytePlus API keys must NEVER appear in logs, error messages, or HTTP responses.
2. `ARK_API_KEY` must be read from environment — never hardcoded.
   (Use `ARK_API_KEY` — this matches the official SDK `github.com/volcengine/ark-runtime-go`.)
3. User-supplied prompts must be validated for length and sanitized before logging.
4. Output media URLs from BytePlus are short-lived — copy to durable storage immediately.
5. All file uploads must be validated server-side (type, size, magic bytes).
6. JWT secrets must be loaded from environment, never from code.
7. Error responses must not expose internal system details (stack traces, SQL errors).

### Architecture
1. Business logic belongs in facades, not handlers.
2. Handlers must never import repository or service packages directly.
3. Database migrations run separately from application startup (no AutoMigrate in prod).
4. The video generation job ID exposed to the frontend is always an application UUID,
   never the BytePlus task ID.
5. Provider-specific fields must not reach the API response envelope.

### Code Style
1. No comments that describe WHAT the code does — only document non-obvious WHY.
2. Return errors up the call stack; wrap with context at layer boundaries.
3. No `interface{}` / `any` in API response types — use concrete structs.
4. No unused imports, variables, or exported symbols.
5. All exported functions must be testable without a live database or live provider.

### Do NOT
- Run `AutoMigrate` in main.go or production startup
- Connect to production databases during tests
- Call BytePlus generation endpoints in tests
- Implement video-generation features before the preparation phase is complete
- Print secrets or full provider payloads to stdout

## Environment Setup

```bash
cp .env.example .env
# Edit .env with local database URL and development credentials

go get github.com/gofiber/fiber/v2
go get gorm.io/gorm gorm.io/driver/postgres
go get github.com/joho/godotenv
go get github.com/golang-jwt/jwt/v5
go get github.com/google/uuid
go mod tidy

# Start dev server
go run .
```

## Agent Coordination

See `AGENTS.md` for agent responsibilities and handoff rules.
The Coordinator (Agent A) owns all cross-cutting architecture decisions.
Agents must not edit overlapping files concurrently.

## Frontend Contract

The frontend API contract lives at `../ez-gen-web/docs/engineering/api-contract.md`.
The backend-authoritative version lives at `docs/engineering/video-generation-api-contract.md`.
These must be kept in sync. The backend version is authoritative for implementation.
