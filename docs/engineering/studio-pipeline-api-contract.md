# Studio one-click pipeline API

Backend authoritative. All routes use authenticated `/open/v1`, standard `data/requestId` envelope, owner-scoped 404, `Cache-Control: no-store`. No provider identifiers in responses.

`POST /studio-pipelines` accepts optional `referenceImageId` UUID (upload) or optional `characterAssetId` UUID (library), mutually exclusive. Neither selects text-to-image. `characterPrompt` is required 1–2000 Unicode characters except library, where it may be empty; `scenePrompt` is always required 1–2000 Unicode characters, `dialogue` optional up to 1000 characters (JSON-quoted representation also limited to 1700 characters to protect the provider prompt budget; combined scene/dialogue/reference controls must fit the 2000-character provider budget and are validated before acceptance), `idempotencyKey` UUID required. Defaults: `visualStyle=cinematic_realistic`, `aspectRatio=9:16`, `durationSeconds=5`, `modelId=violet-motion-2`, `quality=standard`. Styles: cinematic_realistic/3d_animation/anime/illustration/clay. Model/settings must be supported by the actual configured registry. Dialogue requires native audio support. Whitespace-only prompts fail. Dialogue is preserved exactly, never silently shortened. A long dialogue may not fit the selected video duration; completion is not guaranteed by the provider.

The request creates a durable pipeline (201). Creation and retry are rate limited per authenticated owner, and queued/processing pipeline count is bounded by the configured per-user concurrency limit under a transaction-scoped owner advisory lock. Repeating the same owner/key/normalized payload returns the same pipeline (201); a changed payload returns 409 IDEMPOTENCY_CONFLICT. Replay does not depend on current credentials or upload expiration. Upload must initially be owned, unexpired and an image. Other errors: 400 VALIDATION_ERROR, 404 NOT_FOUND, 422 IMAGE_EXPIRED/MODEL_NOT_SUPPORTED, 429 RATE_LIMITED, 503 IMAGE_UNAVAILABLE, 500 INTERNAL_ERROR.

`GET /studio-pipelines?page=1&limit=20` returns `{items,total,page,limit}`; limit 1–50. `GET /studio-pipelines/:id` returns one pipeline. `POST /studio-pipelines/:id/retry` accepts `{}` and resumes the same pipeline, returns 200. Active replay is idempotent. Safe asset-save stages resume without generation. A definitively failed known video can retry using a new durable attempt key while retaining the character. Unknown/interrupted/timeout paid outcomes require attention and cannot retry (409 CONFLICT). No automatic paid regeneration.

Pipeline DTO: `sourceMode` (`upload`/`text`/`library`, derived for legacy snapshots), `id`, `status`, `stage`, `canRetry`, `characterPrompt`, `scenePrompt`, `dialogue`, `failureCode:null|string`, `failureMessage:null|string`, `characterJobId:null|UUID`, `characterAssetId:null|UUID`, `videoJobId:null|UUID`, `videoAssetId:null|UUID`, `characterMediaUrl:null|string`, `characterMediaUrlExpiresAt:null|RFC3339`, `videoMediaUrl:null|string`, `videoMediaUrlExpiresAt:null|RFC3339`, `createdAt`, `updatedAt` (UTC RFC3339).

Status: queued/processing/succeeded/failed/needs_attention. Stage: image/save_character/video/save_video/done. Nullable fields always present. Prompts are the immutable requested brief. Signed URLs are generated only from owned durable assets. No fabricated progress/ETA.

`GET /studio-pipeline-capabilities` returns `{enabled,textEnabled,libraryEnabled,libraryAspectRatios:string[],libraryDurations:number[],supportsDialogue,unavailableReason:null|string,aspectRatios:string[],durations:number[]}`. Enabled requires configured image credentials/model and video credentials and reference-image support in the configured default video model. Audio support reflects the actual registry. No provider IDs are exposed.

Server workers own the sequence. Upload uses img-to-img; no reference uses text-to-image, both full_body single view. Library starts directly at `video` with the selected asset and no image generation or image-model requirement. Library source must be an owned, nondeleted generated character image with a durable key, no upload linkage, and a succeeded character image job with matching output key. Other-owner/deleted assets return 404; invalid provenance returns 409 ASSET_SOURCE_NOT_READY. Replays precede source availability and capability revalidation. New snapshots freeze sourceMode; old upload snapshots remain supported. `enabled`, `aspectRatios`, and `durations` retain upload compatibility; `textEnabled` depends on image generation without requiring upload support; library availability/output arrays depend only on video support. `unavailableReason` describes upload availability. Image is saved to Library with deterministic UUID; a concurrently pre-saved matching owned job asset is reused after provenance validation. Video receives ONLY the generated or selected succeeded owned image asset in reference-image mode, not the uploaded source, and exact double-quoted spoken dialogue with native audio when requested. The generated video is then saved to Library with deterministic UUID. No project/scene is created. Video failures retain saved characters. Provider outputs remain in durable application storage.

Migration `019_studio_pipeline.sql` adds the aggregate and expands the existing Library asset-type constraint to include video. It must be applied separately before use; application startup never applies it. Pipeline snapshots freeze normalized inputs, resolved image model and video registry capability config; changing runtime models does not alter queued/retried requests. Leases use row locks + SKIP LOCKED, per-claim UUID and CAS release, 10-minute lease versus 2-minute worker deadline; no network calls occur in database transactions. Stable child idempotency keys close crash windows between child creation and pointer persistence. Expired child submission/recovery is never automatically resubmitted.

Provider evidence: official BytePlus create/get-video-generation-task API documentation, retrieved 2026-10-22 by coordination research; dialogue uses double quotes and generate_audio=true. No paid generation validation was run.

## Actionable pipeline validation

Facade validation uses HTTP 400 `VALIDATION_ERROR`, a safe actionable Thai `message`, and `details: [{field,reason,message}]`. Stable reasons: `invalid_uuid`, `conflicting_sources`, `required`, `too_long`, `dialogue_encoding_budget`, `prompt_budget`, `invalid_option`, `invalid_image`. Fields identify `referenceImageId`, `characterAssetId`, `idempotencyKey`, `characterPrompt`, `scenePrompt`, `dialogue`, or `visualStyle`. Create-request parser/schema rejection also uses safe structured details as specified below; other endpoints retain their existing validation behavior.

`prompt_budget` identifies `scenePrompt` and explains that scene and dialogue together exceed the compiled prompt budget; shorten either and resubmit. Individual scene/dialogue limits do not guarantee the combined compiled request fits 2,000 Unicode characters, including rendering/reference/audio controls. `dialogue_encoding_budget` identifies `dialogue` and asks the user to reduce length, quotation marks, or newlines because JSON quoting also consumes budget. Neither dialogue nor controls are silently removed. No provider diagnostics, submitted text, or signed URLs are returned in validation errors. Valid requests, normalization, existing request hashes/replays, source selection, and provider budgets are unchanged.

## Draft and final rendering (2026-11-02)

`POST /studio-pipelines` additionally accepts optional `draft` (default false).
The flag is included in the immutable JSON snapshot and request identity, then
forwarded to the video child job. Draft support follows the resolved provider
model, including configuration overrides; unsupported combinations return 422
`MODEL_NOT_SUPPORTED`. Historical snapshots without the flag remain non-draft.

Capabilities additionally include `models`, an ordered array of records with
`id`, resolved `label`, `supportsDraft`, and the existing capability fields
`enabled`, `textEnabled`, `libraryEnabled`, `supportsDialogue`, `aspectRatios`,
`durations`, `libraryAspectRatios`, `libraryDurations`, `unavailableReason`.
Each record describes that model rather than the default model. Labels identify
Seedance version/variant; aliases may share the same resolved provider.

Pipeline results additionally contain `isDraft`, `modelId`, `modelLabel`,
`aspectRatio`, `durationSeconds`, `quality`, `canCreateFinal`, `sourceDraftId`
(nullable application pipeline UUID), `draftExpiresAt` (nullable UTC timestamp),
and `finalUnavailableReason` (nullable safe Thai explanation). Provider task IDs
and provider model routing IDs are never exposed. Draft state is independent of
processing status: a succeeded draft remains a draft.

`POST /studio-pipelines/:id/finalize` accepts only `{ "idempotencyKey": "<UUIDv4>" }`
and returns a new PipelineDTO with HTTP 201. The server authorizes the original
pipeline and child job to the authenticated owner; it requires a succeeded draft,
provider provenance, retained task ID, enabled video generation, and an unexpired
provider draft. It freezes the original model and settings, reuses the character,
and creates a separate video-only pipeline with `isDraft:false`, `quality:"high"`
and `sourceDraftId` linking the original. The provider request refines the stored
draft task at 1080p, rather than resubmitting its prompt/reference. The original
draft is preserved. Unknown body fields are rejected with 400 `VALIDATION_ERROR`.

The endpoint is idempotent for the same source and key, including after draft
expiry; reuse against another source returns 409 `IDEMPOTENCY_CONFLICT`. Foreign
sources return 404 `NOT_FOUND`, ineligible state/provenance returns 409 `CONFLICT`,
expired drafts return 409 `DRAFT_EXPIRED`, disabled generation returns 503
`IMAGE_UNAVAILABLE`; existing rate-limit behavior remains. Expiry is based on
source job creation and the provider retention boundary (7 days before
2026-10-28 02:00 UTC, 14 days on/after). It is checked again at job submission;
expiry before submission becomes pipeline failure `DRAFT_EXPIRED`. Retry retains
the frozen draft mode and source identity. No schema change is required.


### Create-request parser and schema details (2026-11-02)

`POST /studio-pipelines` now returns HTTP 400 `VALIDATION_ERROR` with at most eight
safe `details: [{field,reason,message}]` for parser/schema failures. The top-level
`message` equals the first detail's fixed Thai explanation. Existing facade
validation details and HTTP 422 model-combination errors remain unchanged.

| Fields | Schema/parser reasons |
| --- | --- |
| `referenceImageId`, `characterAssetId`, `idempotencyKey` | `invalid_uuid`, `required` when required by schema, `invalid_type` |
| `characterPrompt`, `scenePrompt`, `dialogue` | `required` when required by schema, `too_long`, `invalid_type` |
| `visualStyle`, `aspectRatio`, `modelId`, `quality` | `invalid_option`, `invalid_type` |
| `durationSeconds` | `out_of_range`, `invalid_type` |
| `draft` | `invalid_type` |
| `body` | `malformed_json` |

Fields use canonical JSON names, including `referenceImageId`, not Go struct
names. Type failures identify a known field when the JSON decoder supplies it;
malformed/unrecognized parser failures use `body/malformed_json`. Messages never
include submitted values, prompts, raw decoder/validator errors or provider data.
Malformed input asks the user to reload and retry. Duration schema allows 4–15
seconds (zero/omitted keeps the existing default), `characterAssetId` accepts canonical UUID identifiers (including v4, v5 and v7); `referenceImageId` and create/finalize `idempotencyKey` still require v4,
and optional settings/draft remain optional. Existing unknown-field handling on
create is unchanged; finalize still rejects unknown body fields.


### Library asset UUID compatibility (2026-11-02)

`characterAssetId` validates a canonical UUID without restricting its version.
Pipeline `saveAsset` intentionally creates deterministic UUIDv5 identifiers with
`uuid.NewSHA1`; returned owned character IDs can therefore be passed unchanged
from the Library to a new pipeline. UUIDv4/v7 character IDs also pass syntax
validation. This does not change ownership or provenance checks: missing,
foreign or nil UUID asset lookups return 404 `NOT_FOUND`; malformed/noncanonical
identifiers return 400 `VALIDATION_ERROR` with `characterAssetId/invalid_uuid`.
`referenceImageId` and both create/finalize `idempotencyKey` remain UUIDv4-only.
No data rewrite, schema migration, provider request or Web payload change is
required.

## Thai speech hints (2026-11-02)

No request or response fields change. For the resolved Seedance 2.5 provider,
Thai-dominant dialogue receives a `{Thai: <JSON-quoted original>}` speech hint
with clear native pronunciation and an unhurried conversational pace. Language
classification compares meaningful Thai letters with other letters; punctuation,
marks, digits and Thai repetition/abbreviation signs do not contribute. An English
sentence with a Thai brand or an ambiguous equal mix keeps the generic template.
Other resolved models and dialogue without a Thai majority retain current speech
formatting. This is a conservative language hint, not a language-detection API.

Raw dialogue, including English names, quotes, newlines and whitespace, remains
unchanged in the request snapshot. The prompt does not translate, silently trim,
or mute music. The full speech template and role/scene controls share the existing
2,000-rune limit, checked by the same compiler during acceptance and child-video
creation. Overflows return the existing scenePrompt/prompt_budget validation
rather than silently dropping text. The individual input/encoding caps remain.
No server duration heuristic blocks dialogue. Frontend duration advice is an
estimate; it does not change input text or promise complete speech or perfect
lip-sync. Actual pronunciation quality requires listening to provider output.

Provider basis: [official Seedance 2.5 tutorial](https://docs.byteplus.com/en/docs/ModelArk/seedance-2-5)
and [create-task API](https://docs.byteplus.com/en/docs/ModelArk/create-video-generation-task-api),
retrieved through indexed primary content 2026-11-02; no paid generation test.

### Confirmed terminal provider failures (2026-11-02)

A terminal failed/cancelled video job with both a nonempty accepted provider task
ID and provider status `failed`/`cancelled` is a confirmed generation failure.
Legacy generic `PROVIDER_ERROR` (and submission/interruption uncertainty codes)
with this evidence becomes pipeline `failed`, code
`PROVIDER_GENERATION_FAILED`, with fixed safe Thai copy. Missing acceptance or
terminal evidence retains `needs_attention`; submission uncertainty never
silently becomes a retryable failure.

`PROVIDER_OUTPUT_REJECTED` identifies generated-output policy rejection, without
claiming a specific face/copyright/prompt cause. It uses fixed Thai guidance to
adjust the scene/reference and start a new request. Both this code and
`PROVIDER_GENERATION_FAILED` expose `canRetry:false`; direct retry is rejected
with 409 `CONFLICT`, avoiding resubmission of the same paid request.

For already-persisted `needs_attention` aggregates, GET/list may project an owned
child's affirmative terminal evidence as `failed` with a safe generation-failed
message. Projection copies the aggregate first, performs no repository write,
preserves the underlying historical state and always keeps `canRetry:false`.
Foreign child lookup cannot authorize projection. This is an observation-only
compatibility behavior, not reconciliation or a database repair.
