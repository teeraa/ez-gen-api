# Repository Map — Agent B Deliverable

> Date: 2026-10-12
> Status: **Baseline** — reflects the state after environment preparation phase
> Branch: (no git repository yet — initialize with `git init`)

---

## 1. Repository State at Baseline

The `ez-gen-api` repository was an **empty directory** at the start of the preparation phase.
The directory structure, module, and documentation were created during environment preparation.
No product features have been implemented.

### Git Status

No git repository initialized. Recommended first steps:
```bash
git init
git add .
git commit -m "chore: initialize Go backend project skeleton"
```

---

## 2. Directory Structure

```
ez-gen-api/
├── main.go                          # Entry point — Fiber app bootstrap
├── go.mod                           # Module: github.com/ez-gen/ez-gen-api (Go 1.26)
├── go.sum                           # (not yet present — run go mod tidy)
├── Dockerfile                       # Multi-stage Docker build
├── .env.example                     # Environment variable template
├── .gitignore
├── CLAUDE.md                        # Claude Code agent instructions
├── AGENTS.md                        # Agent roster and handoff rules
│
├── apps/
│   ├── models/                      # GORM model structs
│   ├── repositories/                # Database access layer
│   │   └── repository_open_api/     # (placeholder — future: user/project repos)
│   ├── facades/                     # Business logic orchestration
│   │   └── facade_open_api/         # (placeholder — future: generation facades)
│   ├── handlers/                    # HTTP request/response binding
│   │   └── handler_open_api/        # (placeholder — future: generation handlers)
│   ├── services/                    # External integrations
│   ├── dto/                         # Response data transfer objects
│   │   └── dto_open_api/
│   ├── requests/                    # Validated input structs
│   │   └── request_open_api/
│   ├── crons/                       # Scheduled background workers
│   ├── db/                          # Database connection setup
│   ├── utils/                       # Shared utilities
│   └── tests/                       # Test helpers and shared fixtures
│
├── routes/
│   ├── api/                         # Internal service routes
│   ├── open_api/                    # User-facing authenticated routes
│   └── oauth2/                      # OAuth2 flow routes
│
├── middlewares/                     # Fiber middleware (auth, logging, rate limit)
├── pkgs/
│   └── configs/                     # Application configuration loader
├── migrate/                         # Migration runner CLI
├── migrations/                      # SQL migration files
├── cmd/                             # Additional CLI commands
├── tools/                           # Developer tools and scripts
├── scripts/                         # Build and CI scripts
├── assets/                          # Static assets (e.g., email templates)
│
└── docs/
    ├── engineering/                 # Architecture and design documents (this dir)
    ├── reference/                   # External reference materials
    ├── api-spec/                    # OpenAPI / Swagger specs
    ├── data-dictionary/             # Database field glossary
    └── iso/                         # ISO/compliance documents
```

---

## 3. Key Files

### main.go

Minimal Fiber bootstrap. Current behavior:
- Loads `.env` via godotenv
- Creates a Fiber app
- Registers `GET /health` → `{"status": "ok"}`
- Listens on `PORT` (default 8080)

**Not yet implemented:** route groups, database connection, middleware, authentication.

### go.mod

```
module github.com/ez-gen/ez-gen-api
go 1.26
```

**Dependencies not yet resolved.** Run:
```bash
go get github.com/gofiber/fiber/v2
go get gorm.io/gorm gorm.io/driver/postgres
go get github.com/joho/godotenv
go get github.com/golang-jwt/jwt/v5
go get github.com/google/uuid
go mod tidy
```

### .env.example

Documents all required environment variables. Copy to `.env` for local development.
See variable groups: Server, Database, JWT, ModelArk, Storage, CORS, Rate Limiting.

---

## 4. Companion Repository

The Next.js frontend (`ez-gen-web`) is at:
```
../ez-gen-web/
```

The frontend preparation phase is complete. See `../ez-gen-web/CLAUDE.md` for its rules.
The frontend expects this API at `API_URL=http://localhost:8080`.

---

## 5. Route Groups (Proposed)

| Group | File | Prefix | Description |
|---|---|---|---|
| Internal | `routes/api/` | `/api/v1` | Service-to-service calls |
| Open API | `routes/open_api/` | `/open/v1` | User-facing authenticated endpoints |
| OAuth2 | `routes/oauth2/` | `/oauth` | OAuth2 authorization flow |

Video-generation endpoints go in **Open API**.

---

## 6. External Dependencies (Proposed)

| Package | Purpose | Status |
|---|---|---|
| `github.com/gofiber/fiber/v2` | HTTP framework | Proposed |
| `gorm.io/gorm` | ORM | Proposed |
| `gorm.io/driver/postgres` | PostgreSQL driver | Proposed |
| `github.com/joho/godotenv` | `.env` file loading | Proposed |
| `github.com/golang-jwt/jwt/v5` | JWT tokens | Proposed |
| `github.com/google/uuid` | UUID v4 generation | Proposed |
| `github.com/go-playground/validator/v10` | Request validation | Proposed |

Run `go get <package>` for each, then `go mod tidy`.

---

## 7. Build and Test Status

| Check | Status | Notes |
|---|---|---|
| `go build ./...` | **BLOCKED** | Dependencies not installed (`go mod tidy` required) |
| `go test ./...` | **N/A** | No tests written yet |
| `go vet ./...` | **BLOCKED** | Requires build to succeed |
| Docker build | **BLOCKED** | Requires `go.sum` |

Baseline build will pass once `go mod tidy` resolves dependencies.

---

## 8. Scripts and Tools

None implemented yet. Planned:
- `scripts/migrate.sh` — run pending migrations
- `scripts/seed.sh` — seed development data
- `tools/check-env.sh` — validate required environment variables

---

## 9. Blockers and Open Questions

| Blocker | Impact | Resolution |
|---|---|---|
| `go mod tidy` not run | Cannot compile | Run during environment setup |
| Database not provisioned | Cannot test DB layer | Requires local PostgreSQL |
| No git repository | Cannot track changes | Run `git init` |
| BytePlus model names unknown | Cannot map UI names to API names | See `modelark-integration-research.md` |
| Authentication system not designed | Cannot implement authorized endpoints | See `backend-security.md` |
| Object storage not chosen | Cannot persist generated media | See `video-data-model-proposal.md` |
