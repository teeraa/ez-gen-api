# VIOLET STUDIO — ModelArk Skill Catalog

> Last updated: 2026-10-12
> Covers: all 13 project skills (5 existing + 8 new)

---

## Quick Reference

| Skill | Trigger | Produces |
|---|---|---|
| `modelark-api-research` | Research needed, doc stale, `[UNCLEAR]` items to resolve | Updated research doc |
| `model-capability-registry` | Add/update/deprecate a model | `models.go` constants or DB seed |
| `modelark-provider-integration` | Implement DNA references, draft mode, audio, error classification | `content_builder.go`, extended `mapper.go` |
| `video-generation-lifecycle` | Implement facade + poller lifecycle | Facade methods, poller, `TransitionStatus` |
| `creative-dna-prompt-compiler` | Implement DNA prompt assembly | `apps/services/dna/compiler.go` |
| `reference-assets-and-storage` | Upload handler, output persistence, signed URLs | `apps/services/storage/`, facade persistence |
| `video-generation-security` | Pre-implementation security check, PR review | Security checklist pass/fail |
| `modelark-integration-testing` | Tests after implementation, broken flow investigation | Test files, all passing |
| `integrate-modelark` | *(existing)* First-time SDK setup, basic client | `client.go`, basic `mapper.go`, poller skeleton |
| `add-api-endpoint` | *(existing)* New HTTP endpoint | Request, DTO, repo method, facade, handler, route |
| `database-change` | *(existing)* Schema change | Migration SQL + GORM model |
| `review-go` | *(existing)* Code review | Review findings report |
| `verify-backend` | *(existing)* Build/test verification | Verification report |

---

## New Skills Detail

### 1 · modelark-api-research
**Trigger:** Any `[UNCLEAR — verify]` in engineering docs; starting a new model integration; docs more than 30 days old.
**Does:** Fetches official BytePlus docs and SDK source, resolves open questions, appends research log entry.
**Does NOT do:** Call the generation API, modify any Go files.

### 2 · model-capability-registry
**Trigger:** Adding/updating/deprecating a model; implementing request validation; capability question.
**Does:** Maintains `ModelRegistry` map with per-model constraints; provides `ValidateRequest()` and `Lookup()`.
**Does NOT do:** Make provider calls; update frontend model names.

### 3 · modelark-provider-integration
**Trigger:** Implementing ContentItem arrays beyond basic text-to-video; adding DNA references; draft mode; audio.
**Does:** `ContentBuilder` with per-mode rules and per-model limits; provider error classification; draft mode two-pass.
**Does NOT do:** SDK installation (that's `integrate-modelark`); test writing (that's `modelark-integration-testing`).

### 4 · video-generation-lifecycle
**Trigger:** Implementing `facade_video_generation`; poller; cancellation; stale job recovery.
**Does:** Full state machine from job creation to terminal state; concurrent-safe `TransitionStatus`; async submission; cancellation flow.
**Does NOT do:** Provider HTTP calls (that's `modelark-provider-integration`); output download (that's `reference-assets-and-storage`).

### 5 · creative-dna-prompt-compiler
**Trigger:** Implementing DNA prompt assembly; debugging visual inconsistency; adding Character or Style DNA.
**Does:** Compiles Style prefix + Character anchors + user prompt + scene instructions into a single prompt and ContentItem array; enforces precedence and length limits.
**Does NOT do:** Store DNA config (that's a DB task); guarantee visual consistency.

### 6 · reference-assets-and-storage
**Trigger:** Implementing `POST /open/v1/uploads`; output persistence after generation; signed URL generation.
**Does:** Storage abstraction (`Client` interface, multi-provider); magic byte validation; output download + store; signed URL generation; upload expiry.
**Does NOT do:** Provider polling (that's `video-generation-lifecycle`); security reviews (that's `video-generation-security`).

### 7 · video-generation-security
**Trigger:** Before implementing any generation endpoint; when `security-reviewer` is invoked on generation code; after any provider API change.
**Does:** Verifies prompt length, magic bytes, signed URL usage, API key handling, error sanitization, DNA asset authorization.
**Does NOT do:** Write implementation code; replace general security rules in `.claude/rules/security.md`.

### 8 · modelark-integration-testing
**Trigger:** After implementing any component in `apps/services/modelark/`, `apps/services/dna/`, `apps/crons/`, or the video generation facade.
**Does:** Provides mock infrastructure (MockModelArkClient, MemoryStorage); test suites for mapper, ContentBuilder, registry, DNA compiler, facade lifecycle.
**Does NOT do:** Integration tests against a live provider; handler tests (that's `add-api-endpoint`).

---

## Relationship to Existing Skills

| New Skill | Relationship |
|---|---|
| `modelark-provider-integration` | Extends `integrate-modelark` — read that first |
| `video-generation-lifecycle` | Uses `integrate-modelark`'s Client interface |
| `video-generation-security` | Extends `.claude/rules/security.md` — read that first |
| `modelark-integration-testing` | Tests code produced by all integration skills |
| `reference-assets-and-storage` | `add-api-endpoint` handles the HTTP layer; this handles storage |
