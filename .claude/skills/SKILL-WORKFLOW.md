# ModelArk Integration — Skill Dependency & Workflow Diagram

---

## Dependency Graph

```
                    ┌──────────────────────┐
                    │ modelark-api-research │  Must run first; unblocks model IDs
                    └──────────┬───────────┘
                               │ produces confirmed model IDs + capabilities
                               ▼
                    ┌────────────────────────┐
                    │ model-capability-registry│  ModelRegistry + ValidateRequest()
                    └──────────┬─────────────┘
                               │ provides Lookup(), ModelCapability
              ┌────────────────┼──────────────────────┐
              │                │                      │
              ▼                ▼                      ▼
  ┌──────────────────┐  ┌──────────────────┐  ┌─────────────────────────┐
  │integrate-modelark│  │creative-dna-     │  │reference-assets-        │
  │  (existing)      │  │prompt-compiler   │  │and-storage              │
  └────────┬─────────┘  └────────┬─────────┘  └──────────┬──────────────┘
           │ Client interface     │ Compile()              │ storage.Client
           │ ContentItem types    │ ContentItems           │ SignedURL()
           ▼                     │                        │ PersistOutput()
  ┌──────────────────────┐       │                        │
  │modelark-provider-    │       │                        │
  │integration           │◄──────┘                        │
  │(ContentBuilder,      │                                │
  │ error classifier)    │                                │
  └──────────┬───────────┘                                │
             │ CreateTask, GetTask                        │
             └──────────────────────┐                     │
                                    ▼                     │
                         ┌──────────────────────┐         │
                         │ video-generation-    │◄────────┘
                         │ lifecycle            │
                         │(facade + poller)     │
                         └──────────┬───────────┘
                                    │
                  ┌─────────────────┼──────────────────┐
                  ▼                 ▼                   ▼
       ┌──────────────────┐  ┌──────────────┐  ┌──────────────────────┐
       │video-generation- │  │add-api-      │  │modelark-integration- │
       │security          │  │endpoint      │  │testing               │
       │(review gate)     │  │(HTTP layer)  │  │(verifies everything) │
       └──────────────────┘  └──────────────┘  └──────────────────────┘
```

---

## Sequential Implementation Order

```
Phase 0 — Research (no code)
  1. modelark-api-research           Resolve all [UNCLEAR] items
  2. model-capability-registry       Create ModelRegistry constants

Phase 1 — Provider Layer
  3. integrate-modelark              SDK setup, Client interface, basic mapper
  4. modelark-provider-integration   ContentBuilder, extended mapper, draft mode

Phase 2 — Storage + DNA
  5. reference-assets-and-storage    Storage abstraction, upload handler, output persistence
  6. creative-dna-prompt-compiler    DNA compiler, prompt assembly

Phase 3 — Lifecycle + API
  7. video-generation-lifecycle      Facade orchestration, poller, cancellation
  8. add-api-endpoint                HTTP handlers, routes

Phase 4 — Verification
  9. video-generation-security       Security review gate
 10. modelark-integration-testing    All tests pass
 11. verify-backend                  go build + go vet + go test -race
```

---

## Parallel Work Opportunities

Skills that can be worked on simultaneously (no shared file edits):

| Parallel Track A | Parallel Track B |
|---|---|
| `modelark-provider-integration` | `reference-assets-and-storage` |
| `creative-dna-prompt-compiler` | `modelark-integration-testing` (mock infra) |
| `video-generation-security` (review) | `add-api-endpoint` (handler scaffolding) |

Agents working in parallel must not edit the same file. Coordinate through Agent A.

---

## Data Flow: Single Generation Request

```
POST /open/v1/jobs
     │
     ▼ [add-api-endpoint]
Handler: validate request → call facade
     │
     ▼ [video-generation-lifecycle]
Facade: idempotency check → credit reserve → persist job (queued) → async submit
     │
     ▼ [video-generation-lifecycle / modelark-provider-integration]
submitToProvider():
  DNA compiler.Compile() → ContentBuilder.Build() → CreateTask() → persist (submitted)
     │
     ▼ [video-generation-lifecycle]
Poller: GetTask() every N seconds → TransitionStatus()
     │
     ▼ (status == succeeded) [reference-assets-and-storage]
PersistOutput(): download video_url → upload to storage → update job (succeeded)
     │
     ▼
GET /open/v1/jobs/:id → storage.SignedURL() → return mediaUrl + expiresAt
```

---

## Agent-to-Skill Mapping

| Agent | Primary Skills | Secondary Skills |
|---|---|---|
| `modelark-specialist` | `integrate-modelark`, `modelark-provider-integration`, `video-generation-lifecycle` | `modelark-api-research`, `model-capability-registry` |
| `api-implementer` | `add-api-endpoint`, `video-generation-lifecycle` (facade) | `model-capability-registry` (validation) |
| `test-engineer` | `modelark-integration-testing` | `verify-backend` |
| `security-reviewer` | `video-generation-security` | `.claude/rules/security.md` |
| `go-architect` | `model-capability-registry` (design decision) | `video-generation-lifecycle` (poller design) |
| `database-reviewer` | `database-change` | `video-generation-lifecycle` (TransitionStatus review) |
| No dedicated agent | `modelark-api-research` | Coordinator assigns |
| No dedicated agent | `creative-dna-prompt-compiler` | `modelark-specialist` or new `dna-engineer` agent |
| No dedicated agent | `reference-assets-and-storage` | `api-implementer` with storage focus |
