# VIOLET STUDIO — Go Backend API

The backend API for VIOLET STUDIO, a premium AI video generation platform.
Built with Go · Fiber · GORM · PostgreSQL · BytePlus ModelArk.

## Quick Start

```bash
cp .env.example .env
# Edit .env with your local database URL

go get github.com/gofiber/fiber/v2
go get gorm.io/gorm gorm.io/driver/postgres
go get github.com/joho/godotenv
go mod tidy

go run .
# → listening on :8080

curl http://localhost:8080/health
# → {"status":"ok"}
```

## Documentation

All architecture documentation is in `docs/engineering/`:

| Document | Contents |
|---|---|
| `repository-map.md` | File structure and project layout |
| `baseline-report.md` | Toolchain and build status |
| `backend-architecture.md` | Layered architecture and component rules |
| `modelark-integration-research.md` | BytePlus ModelArk API behavior |
| `video-data-model-proposal.md` | Database schema proposal |
| `video-generation-api-contract.md` | API contract (authoritative backend version) |
| `backend-security.md` | Security rules and trust boundaries |
| `backend-verification-playbook.md` | Test and verification procedures |
| `agent-workflow.md` | Agent chain and implementation workflow |

See `CLAUDE.md` for development rules and `AGENTS.md` for agent responsibilities.

## Status

**Phase: Environment Preparation** — skeleton and documentation complete.
No product features are implemented yet.

The companion frontend (`ez-gen-web`) preparation phase is complete.
