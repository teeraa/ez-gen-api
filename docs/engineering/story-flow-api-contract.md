# Story Flow API (additive, isolated)

Base `/open/v1/story-flow`, existing JWT and `{data,requestId}` envelope.
No production migration is run. Requires migration020. Sources are immutable after
create: start a new project to change inputs; revision is1 throughout this version.
Old projects/scenes/jobs/pipelines/exports are never written by this domain.

- `GET /capabilities`: `{enabled,unavailableReason,models:[{id,name,aspectRatios,qualities,minDuration,maxDuration}],styles,maxScenes:10,maxShots:20}`.
- `GET /projects?page=1&limit=20`: `{items:Project[],total,page,limit}`.
- `POST /projects`: `{title,idea,style,modelId,quality,aspectRatio,sceneCount,moodTags:string[],characterAssetIds:string[],newCharacterPrompt:string,idempotencyKey:UUID}` -> Project.
- `GET /projects/:id`: Project.
- `POST /projects/:id/story`: `{revision:1,idempotencyKey:UUID}` -> Project.
- `POST /projects/:id/storyboard`: `{revision:1,idempotencyKey:UUID,sceneShots:[{sceneIndex:1,shotCount:2},...]}` -> Project.
- `POST /projects/:id/video`: `{revision:1,idempotencyKey:UUID}` -> Project.
- `POST /projects/:id/retry`: `{revision:1,idempotencyKey:UUID,stage:string,shotId?:UUID}` -> Project. Use the project's current stage. For per-shot failures use the shot ID. Retry only `canRetry:true` failures.

Project: `{id,...createInput,revision,status,stage,scenes:Scene[],outputUrl:string|null,downloadUrl:string|null,failureCode,failureMessage,canRetry,completedImages,completedVideos,totalShots,createdAt,updatedAt}`. Creation input idempotencyKey is returned for resumption; it is not a credential.
Scene: `{sceneIndex,title,narrative,location,characters:string[],emotion,continuity,shots:Shot[]}`.
Continuity: `{wardrobe,lighting,characterState}` (bounded strings).
Shot: `{id,sceneIndex,shotIndex,action,characters:string[],location,atmosphere,framing,cameraAngle,cameraMovement,continuity,imagePrompt,videoPrompt,durationSeconds,imageStatus,videoStatus,imageUrl:string|null,videoUrl:string|null,failureCode,failureMessage,canRetry}`.
Indices start at1 and are validated against exact scene/shot counts. Character strings
are selected character IDs (including server-created `new-character`). No provider
task IDs or storage keys are exposed.

Status: `draft|queued|processing|succeeded|failed|needs_attention`.
Stage: `draft|story|story_review|shots|storyboard|storyboard_review|video|export|done`.
Shot status: `pending|submitting|processing|succeeded|failed|needs_attention`.
Review checkpoints use draft status and story_review/storyboard_review stages.
Progress is completed durable images/clips divided by totalShots; no estimated timer percentage.

All POST keys are UUIDs; key replay with different payload ->409 `IDEMPOTENCY_CONFLICT`.
Ownership failures ->404 `PROJECT_NOT_FOUND`; invalid fields ->400
`VALIDATION_ERROR` with `{field,reason,message}` details. Invalid revision/state ->409
`STORY_FLOW_CONFLICT`; missing required provider configuration ->503
`STORY_FLOW_UNAVAILABLE`. Unexpected errors ->500 `INTERNAL_ERROR` with safe text.

Story and shot analysis are separate configured text/chat analysis calls; there is
no model-name default. `ARK_STORY_ANALYSIS_MODEL` explicitly selects an activated
analysis model. API key/base URL use separate optional overrides with existing
analysis configuration fallbacks. Image/video models, prompt version and full inputs
are snapshotted. Model JSON must be structurally valid, complete, have exact counts,
known characters and ordered indices before success is persisted.

One generated storyboard image per shot. Current image adapter accepts one visual
reference: use the first appearing character's owned image and full textual
appearance/continuity for all characters. This is a capability boundary, not a
claim that identity consistency is guaranteed. No contact sheets. Realistic new
characters use the existing current synthetic baseline compiler unchanged; video
prompts independently request filmed photorealistic live action, preserve face/hair/
proportions/wardrobe design, and explicitly replace CG materials/skin/lighting.
Other styles never receive live-action directions.

Workers process one bounded action at a time with database leases and token-guarded
writes. Before external create, persist submitting. Ambiguous create failure or
interrupted submitting is needs_attention; never automatically resubmit. Provider
rejection is shown safely and only explicit retry can create another attempt.
Completed shots survive partial failures and retries. Video creation uses saved
shot prompts/images and does not analyze/split again. Export checks every shot,
uses sceneIndex/shotIndex order and unchanged FFmpeg normalization. Only720p/1080p
export is offered, intersected with registry quality capability. Max20 shots comes
from current processor limit. Signed URLs are refreshed on GET; object keys persist.

Input bounds: title1–120 Unicode characters; idea1–6000; newCharacterPrompt0–2000;
each mood tag1–40, at most5 total including custom tags; at most8 distinct owned
Library character IDs; sceneCount1–10; total shots1–20. All scene count/index and
shot counts are validated server-side. Compiled per-shot image/video prompts are
validated against the existing adapter's conservative2000-character prompt budget
before any images are queued. Overlong/incomplete plans fail analysis explicitly;
no partial plan is saved as ready. Character summaries are returned as
`characterSummaries:[{id,name}]` so the UI can name characters without exposing
storage keys. Project source is immutable in this release. Shot dialogue/audio is
outside this release; planning is visual-only and requests no generated audio.

Provider evidence (official documentation checked2026-10-26):
[create video task](https://docs.byteplus.com/en/docs/modelark/create-video-generation-task-api),
[video tutorial](https://docs.byteplus.com/en/docs/modelark/video-generation-tutorial),
[chat API](https://docs.byteplus.com/en/docs/modelark/chat-api),
[image API](https://docs.byteplus.com/en/docs/modelark/image-generation-api),
[portrait assets](https://docs.byteplus.com/en/docs/modelark/seedance-portrait-asset-guide).
Story Flow uses saved shot image as first_frame with an allowed image-to-video
model. Seedance2.5 models are gated out in this feature until its adaptive-ratio
first-frame behavior can satisfy the selected output contract; legacy registry
behavior is unchanged. Synthetic character rendering does not guarantee provider
acceptance. Seedream generation and storage ownership do not make an image an
approved Seedance portrait asset. Real provider rejection remains visible, with
no automatic same-image retry. These are documentation checks, not live paid calls.

Usage accounting boundary: old ai_usage rows reference legacy generation-job IDs.
Story Flow does not fabricate those jobs or alter that ledger. It keeps separate
project/shot snapshots and provider attempt history but does not add its calls to
the old usage ledger in this release. Analysis prompts for both story and shots,
character compiler version, selected primary appearing character reference and
video provider-task history remain in the private persisted snapshot.

Full owned character descriptions remain in the analysis input snapshot. Per-shot
prompts use pinned deterministic appearance excerpts (up to160 characters per
appearing character), plus the primary owned visual reference and saved continuity.
Excerpts are a bounded supplement, not a guarantee of full semantic preservation;
inspect the saved prompts and storyboard before rendering. Structural action,
continuity and model-written prompts are never silently truncated. If the final
compiled prompt still exceeds the budget, analysis fails before image creation.
Analysis also persists a submitting marker: interruption or an ambiguous network
error becomes needs_attention, with no automatic second paid analysis call.
Invalid schema/count output may be retried explicitly with identical frozen inputs.

Independent-audit recovery corrections: storyboard/character image success requires
fully decoded PNG/JPEG pixels, after compressed-byte, dimension and decoded-pixel
bounds are checked. Header-only or truncated bitmaps fail with IMAGE_INVALID_OUTPUT
and cannot unlock video creation. Stored image MIME and extension match the decoded
PNG/JPEG format.

Analysis errors preserve outcome certainty: definitive HTTP4xx rejection and
completed invalid/empty/truncated-limit or content-filtered model responses become
safe failed states with explicit retry enabled. Retry preserves the frozen model
and analysis prompt. Transport/network/timeouts, HTTP5xx and interrupted submitting
remain needs_attention and never resubmit automatically. Provider payloads, private
URLs and credentials are not retained in public errors.
