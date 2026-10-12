# Skill: modelark-api-research

Research the current state of the BytePlus ModelArk Video Generation API and update
`docs/engineering/modelark-integration-research.md` with verified facts, source links,
and verification dates.

## When to Invoke

- Before implementing any ModelArk integration code for the first time
- When a new model identifier or capability is needed and the registry is incomplete
- When a SKILL.md or engineering doc references a fact marked `[UNCLEAR — verify]`
- When the last research date in the doc is more than 30 days old
- After a BytePlus release announcement that may affect the API

## Inputs

- Optional: specific questions to answer (e.g., "confirm Seedance 2.5 audio support")
- Optional: a list of `[UNCLEAR — verify]` items from other docs to resolve

## Outputs

- Updated `docs/engineering/modelark-integration-research.md` with:
  - Each finding clearly marked as: **CONFIRMED**, **INFERRED**, or **UNRESOLVED**
  - Source URL and date for each confirmed fact
  - Resolved `[UNCLEAR — verify]` items replaced with confirmed values

Do not update any implementation file. Research docs only.

---

## Workflow

### Step 1 — Read Current Research

```
Read: docs/engineering/modelark-integration-research.md
```

Collect all items marked `[UNCLEAR — verify]`. List them before fetching anything.

### Step 2 — Fetch Official Documentation

Fetch in this priority order. Use `WebFetch` for each:

```
1. https://docs.byteplus.com/en/docs/ModelArk/Video_Generation_API
2. https://docs.byteplus.com/en/docs/ModelArk/create-video-generation-task-api
3. https://docs.byteplus.com/en/docs/ModelArk/1890282  (Get task — verify path)
4. https://docs.byteplus.com/en/docs/ModelArk/1890283  (List tasks — verify path)
```

If a page returns empty (SPA rendering issue), note it as **UNRESOLVED — SPA rendering
blocked fetch** and move to the SDK source as fallback.

### Step 3 — Read SDK Source as Ground Truth

The official Go SDK is the most reliable machine-readable source:

```
Package: github.com/volcengine/ark-runtime-go
Key file: arkruntime/model/contentgeneration/oas_schemas_gen.go
```

Read the struct definitions for:
- `CreateContentGenerationsTasksRequest`
- `ContentGenerationTask` (status response)
- `TaskStatus` constants
- `ContentItem` / `Role` enumerations

Cross-reference what the SDK accepts against what the docs describe.

### Step 4 — Verify Model-Specific Facts

For each model in the registry (see `model-capability-registry` skill), confirm:
- Exact BytePlus API model ID string
- Max resolution
- Duration range (min and max seconds)
- `generate_audio` support
- `draft` mode support
- Max reference images
- Max reference videos

Mark findings as CONFIRMED (from SDK struct or doc), INFERRED (from community code),
or UNRESOLVED.

### Step 5 — Check Webhook Behavior

Verify:
- Whether `callback_url` is officially supported
- Webhook payload structure
- Signature/HMAC mechanism (if any)

This is currently UNRESOLVED in the research doc.

### Step 6 — Verify Output URL Lifetime

Find the official TTL for `response.content.video_url`. Currently noted as "~24 hours"
from community sources. Replace with an official source if one exists.

### Step 7 — Write Findings

Update `docs/engineering/modelark-integration-research.md`:
- Add `> Last verified: <date>` to the header
- Replace `[UNCLEAR — verify]` items with confirmed values
- For items that remain unresolved, add `[UNRESOLVED — <reason>]`
- Add a "Research Log" section at the bottom with date, questions answered, sources used

---

## What NOT to Do

- Do not call the BytePlus generation API
- Do not create a task or poll for status
- Do not modify any Go source files
- Do not update `.env.example` with real credentials found in docs
- Do not assume parameters are identical across models without model-specific confirmation

---

## Acceptance Criteria

- [ ] All `[UNCLEAR — verify]` items from the existing doc are addressed (resolved or escalated)
- [ ] Every confirmed fact has a source link and date
- [ ] SDK version and key struct names are recorded
- [ ] Model ID table has been cross-checked against SDK source
- [ ] Research Log entry appended with today's date

---

## Escalation

If the official docs are completely inaccessible (SPA rendering, auth gate):
1. Note every fact sourced from SDK only as **SDK-INFERRED** (highly reliable but not official)
2. Note every fact sourced from community as **COMMUNITY-INFERRED** (treat as hint, verify before use)
3. Add a blocker entry to `docs/engineering/baseline-report.md`:
   > "BytePlus official docs inaccessible via automated fetch. SDK source used as primary reference."

---

## Related Skills

- `model-capability-registry` — consumes the model ID table produced here
- `modelark-provider-integration` — requires confirmed model IDs and ContentItem schemas
- `modelark-integration-testing` — requires confirmed status values and error codes
