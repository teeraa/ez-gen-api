# Subagent setup audit — 2026-10-15

Owner: Agent A. Task: inspect Claude agents and create an auditable Codex team.
Base HEAD: `4e5a68734097bc8e3f1c31c1fcce392018d432f6`.
Initial tracked/untracked git status: clean. Scope: agent definitions, routing and
this audit record; no application behavior or existing Claude definitions changed.

## Existing Claude findings

| Severity | Location | Finding | Disposition |
|---|---|---|---|
| HIGH | `.claude/agents/database-reviewer.md:25,72` | CONCURRENTLY requirement conflicts with unconditional transactional migrations | Codex reviewer explicitly handles exception; Claude fix remains open |
| HIGH | `.claude/agents/test-engineer.md:56-60` | Missing integration DB exits zero, disguising omitted verification | Codex marks missing prerequisite BLOCKED/NOT_RUN; Claude fix remains open |
| MEDIUM | `.claude/agents/api-implementer.md:107-112` | Literal PASS template lacks execution evidence; race check excludes non-app packages | Codex evidence contract and root race gate |
| MEDIUM | `.claude/agents/modelark-specialist.md:10,16-17,68,92` | Poller path and SDK import/interface differ from current code | Codex requires source/SDK inspection, no snippet copying |
| MEDIUM | `.claude/agents/database-reviewer.md:46-52,83` | Migration numbering and model_configs assumptions do not reflect actual schema | Codex reviews actual migration/model files |
| MEDIUM | `AGENTS.md` preparation phase; `CLAUDE.md:91-96` | Historical status can be mistaken for current readiness | AGENTS now labels status historical and requires current checks |

This is an instruction/configuration audit, not a full product security audit.
The existing six Claude definitions remain unchanged, so their findings remain
open for a separate scoped correction. New setup does not authorize product work
or waive preparation prerequisites.

## Delivery and independent review

Eight project-scoped TOML definitions: repo-explorer, go-architect,
api-implementer, database-reviewer, modelark-specialist, security-reviewer,
verification-engineer and independent-auditor. Coordinator is the main agent.
Routing and evidence contract: `.codex/agents/README.md`.

Independent reviewer thread: `/root/agent_auditor`; separate invocation, no edits.
The current orchestration tool does not expose a custom-role selection argument,
so this audit used a generic subagent with an explicit independent review prompt.
It did not claim the newly registered custom role was invoked.
Reviewer found no blocking instruction findings in the eight definitions/README.
Instruction-driven scenarios: missing execution evidence -> BLOCKED; missing
required integration environment -> BLOCKED/NOT_RUN; post-review source change ->
invalidate prior verdict. These are reviewer assessments, not live fixture evals.

## Verification checklist

- [x] Read all six existing Claude agent definitions, rules references and workflow.
- [x] Consult current official standalone custom-agent schema.
- [x] Parse all eight TOML files with Python 3.9 `pip._vendor.tomli`; required keys,
  nonempty strings and filename/name match all PASS (command exit 0).
- [x] `git diff --check` PASS (exit 0).
- [x] Independent instruction review complete; no blocking findings in new setup.
- [ ] Runtime discovery of custom names in a new client session: NOT_RUN.
- [ ] Live behavior evaluation fixtures: NOT_RUN; cases supplied in README.
- [ ] Human documentation review: PENDING, required by AGENTS.md handoff rule 3.
- Go build/vet/tests: NOT_APPLICABLE to this configuration/documentation-only change.

Default Python lacks tomllib; the first parser attempt failed. A second attempt
using the already-installed pip vendored TOML parser passed; no package installed.
Installed CLI reports `codex-cli 0.162.1`. CLI version/schema generation does not
prove custom-role runtime discovery. Reviewer no-edit rules are behavioral;
permissions inherit from parent and are not separately enforced by these files.

## Sources and interpretation

Interpreted “luaren tan” as Lauren Tan (poteto). Adapted engineering depth,
verifiable behavior and independent interrogation from the primary pstack README:
https://github.com/cursor/plugins/tree/main/pstack (checked 2026-10-15).
No pstack installation, model changes or auto-merge enabled.
Custom-agent schema: https://learn.chatgpt.com/docs/agent-configuration/subagents
(checked 2026-10-15). Eight standalone files require name, description and
 developer_instructions; no additional role registration config is needed per
that documentation. Runtime remains unverified in this session.

## Reviewed artifact identity

SHA-256 manifest includes new untracked definitions and README, plus AGENTS.md.
This report is the evidence container and is excluded from its own hash manifest.
Documentation acceptance remains pending human review.

| File | SHA-256 |
|---|---|
| `AGENTS.md` | `452993d0d31dcce334f4e03d272872e245f005d799ec6cb2f5ff79b6224c7f90` |
| `.codex/agents/README.md` | `f4be49aad3b524057f879717e6da562c025ad1254cca962754a157e39ef2533d` |
| `.codex/agents/api-implementer.toml` | `b8889f7147d76a1ad414228b7b61a3da8d85d7d8785a5db87d2ba60b4208b4a3` |
| `.codex/agents/database-reviewer.toml` | `6efba3db9e33f3a8daae8cd8fc9df1f08db41cf942425906b8b3088410c23baf` |
| `.codex/agents/go-architect.toml` | `0d9828e5e13820503d7ad0dbd030082354fafe38425a9851237285a57d639608` |
| `.codex/agents/independent-auditor.toml` | `435b37fc1593ab05dcce8f1c47bed1ba2a9df1cb1d7a4a858ba15e945996bc64` |
| `.codex/agents/modelark-specialist.toml` | `9d4d02352c5545736afd769462d1b6c3d76ba3fa653712d04183b8f32cdbc28c` |
| `.codex/agents/repo-explorer.toml` | `f7689ed0c82c0a98eeac4ac4e8b232c7379ea7c57acc7a4995dcb6ca5e41e02f` |
| `.codex/agents/security-reviewer.toml` | `e6c8b99f6ebd28f18217dc2bad22a5539b744dcb24db6e9c0c88a99aaebf8bea` |
| `.codex/agents/verification-engineer.toml` | `903d22866517ac93e179e46f227d3cc22576c805baeeaed88d634f24f4a7efc5` |
