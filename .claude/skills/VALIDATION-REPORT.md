# Skill Validation Report

> Date: 2026-10-12
> Scope: 8 new ModelArk integration skills
> Validator: Agent A (Coordinator)

---

## 1. Files Created

```
.claude/skills/modelark-api-research/SKILL.md         ✓
.claude/skills/model-capability-registry/SKILL.md     ✓
.claude/skills/modelark-provider-integration/SKILL.md ✓
.claude/skills/video-generation-lifecycle/SKILL.md    ✓
.claude/skills/creative-dna-prompt-compiler/SKILL.md  ✓
.claude/skills/reference-assets-and-storage/SKILL.md  ✓
.claude/skills/video-generation-security/SKILL.md     ✓
.claude/skills/modelark-integration-testing/SKILL.md  ✓
.claude/skills/SKILL-CATALOG.md                       ✓  (deliverable)
.claude/skills/SKILL-WORKFLOW.md                      ✓  (deliverable)
.claude/skills/VALIDATION-REPORT.md                   ✓  (this file)
```

---

## 2. Checks Performed

### Structure Check

| Skill | Has trigger | Has inputs/outputs | Has workflow | Has acceptance criteria | Has related skills |
|---|---|---|---|---|---|
| `modelark-api-research` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `model-capability-registry` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `modelark-provider-integration` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `video-generation-lifecycle` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `creative-dna-prompt-compiler` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `reference-assets-and-storage` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `video-generation-security` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `modelark-integration-testing` | ✓ | ✓ | ✓ | ✓ | ✓ |

### Duplication Check

| Potential overlap | Resolution |
|---|---|
| `modelark-provider-integration` vs `integrate-modelark` | Split by scope: existing skill covers SDK setup; new skill covers DNA references, draft mode, error classification |
| `video-generation-security` vs `.claude/rules/security.md` | Explicit in skill: "Read that file first. This skill ADDS to those rules." |
| `video-generation-lifecycle` vs `integrate-modelark` (poller) | Existing skill has skeleton only; lifecycle skill has full concurrent-safe implementation |
| `reference-assets-and-storage` vs `add-api-endpoint` | HTTP layer separated: `add-api-endpoint` handles handler scaffold; storage skill handles storage abstraction and persistence |

### Nonexistent File References Check

| Skill | References | Exists? |
|---|---|---|
| All skills | `docs/engineering/modelark-integration-research.md` | ✓ |
| All skills | `docs/engineering/video-data-model-proposal.md` | ✓ |
| All skills | `CLAUDE.md`, `.claude/rules/security.md` | ✓ |
| `model-capability-registry` | `apps/services/modelark/models.go` | ✗ — proposed, to be created |
| `modelark-provider-integration` | `apps/services/modelark/content_builder.go` | ✗ — proposed, to be created |
| `video-generation-lifecycle` | `apps/facades/facade_video_generation/` | ✗ — proposed, to be created |
| `creative-dna-prompt-compiler` | `apps/services/dna/compiler.go`, `apps/models/character_dna.go` | ✗ — proposed |
| `reference-assets-and-storage` | `apps/services/storage/storage.go` | ✗ — proposed, to be created |
| `modelark-integration-testing` | `apps/tests/mock_modelark.go`, `apps/tests/mock_storage.go` | ✗ — proposed, to be created |

All proposed files are marked as such in their respective skills. No skill
claims a file exists when it does not.

### Live API Call Check

All 8 skills explicitly state they do not call BytePlus generation endpoints.
`modelark-api-research` uses `WebFetch` for documentation only (read-only).

### Secret Exposure Check

No skill contains placeholder API keys, real credentials, or instructions
to print secrets. `ARK_API_KEY` is referenced by name only.

---

## 3. Skill Merge / Split Decisions

| Decision | Rationale |
|---|---|
| Keep `modelark-api-research` separate | Research is a distinct workflow requiring `WebFetch`; merging into any implementation skill would conflate research with coding |
| Keep `model-capability-registry` separate | Registry management (add/update/deprecate models) is a recurring operational task; merging with provider integration would create a single too-broad skill |
| Keep `creative-dna-prompt-compiler` separate | DNA prompt compilation is a standalone domain with its own data model; it could be invoked without any other integration change |
| Keep `video-generation-security` separate | Security review is an independent verification gate, not an implementation step; it should be invokable at any time without producing code |
| Considered merging `reference-assets-and-storage` into `video-generation-lifecycle` | Rejected — storage abstraction has independent value (upload handler) outside the poller/lifecycle flow |

---

## 4. Unresolved Questions

These questions are inherited from the research phase and must be answered
before implementing the marked skills:

| Question | Blocking Which Skill | Escalation |
|---|---|---|
| Exact lifetime of BytePlus output URLs (currently "~24h") | `reference-assets-and-storage` | Run `modelark-api-research` |
| Whether `violet-fast` maps to same BytePlus model as `violet-motion-2` | `model-capability-registry` | Product decision + `modelark-api-research` |
| Webhook payload format and HMAC signature | `video-generation-lifecycle` (optional optimization) | Non-blocking; polling is safe default |
| Authentication system (JWT self-issued vs Clerk) | `video-generation-lifecycle` (userID in all facades) | Architecture decision; see `backend-security.md` |
| Object storage provider (S3 / TOS / R2) | `reference-assets-and-storage` | Business decision; affects env vars |
| Seedance 2.0 Mini capability details | `model-capability-registry` | Run `modelark-api-research` |
| Whether `optimize_prompt: false` is the correct default for DNA prompts | `creative-dna-prompt-compiler` | Test empirically once API access is available |

---

## 5. Official Documentation References

| Fact | Source | Verified Date |
|---|---|---|
| Base URL `https://ark.ap-southeast.bytepluses.com/api/v3` | `github.com/volcengine/ark-runtime-go` SDK | 2026-10-11 |
| Task ID prefix `cgt-...` | Community integrations | 2026-10-12 (not official) |
| Provider status values: `queued`, `running`, `succeeded`, `failed`, `cancelled` | SDK `TaskStatus` constants | 2026-10-11 |
| Output URL ~24h lifetime | Community sources | 2026-10-12 (not official — verify) |
| Seedance 2.5 model ID `dreamina-seedance-2-5-260628` | SDK source | 2026-10-11 |
| Seedance 2.0 Fast model ID `dreamina-seedance-2-0-fast-260128` | SDK source | 2026-10-11 |
| ContentItem ordering: text first | SDK + community | 2026-10-12 |
| `first_frame` role for image-to-video | SDK source | 2026-10-11 |
| `generate_audio` Seedance 2.5 only | SDK source | 2026-10-11 |
| Draft mode via `"draft": true` + `"draft_task"` ContentItem | SDK source | 2026-10-11 |

---

## 6. Recommended Next Steps

### Immediate (before any implementation)

1. Run `modelark-api-research` skill to resolve all `[UNCLEAR — verify]` items.
2. Confirm `violet-fast` vs `violet-motion-2` product decision.
3. Choose object storage provider and configure `.env.example` `STORAGE_PROVIDER`.
4. Decide authentication system (JWT self-issued vs external).

### Implementation Order

Follow the sequential order in `SKILL-WORKFLOW.md` Phase 0 → Phase 4.
Do not skip Phase 0 (research + registry) before writing provider code.

### Agent Gaps

Three skills currently have no dedicated agent:
- `modelark-api-research` — assign to Coordinator or create a `research-specialist` agent
- `creative-dna-prompt-compiler` — assign to `modelark-specialist` or create a `dna-engineer` agent
- `reference-assets-and-storage` — assign to `api-implementer` with storage expertise

### Skills Not Yet in System-Reminder

`creative-dna-prompt-compiler`, `reference-assets-and-storage`, `video-generation-security`,
and `modelark-integration-testing` are now available in `.claude/skills/` but may not appear
in the system reminder immediately. They will appear once Claude Code indexes the new skill files.
