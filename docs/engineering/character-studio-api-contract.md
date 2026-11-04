# Character Studio — phase one, 2026-10-15

Scope: authentication → character image generation → reusable personal Asset Library.
Projects, planning, scenes, and export remain existing legacy features and are not
part of this delivery. All routes below are `/open/v1`; standard `{data,requestId}`
success and `{code,message,requestId}` error envelopes. All except register/login
require `Authorization: Bearer <HS256 JWT>` issued by this API. Missing/expired
credentials return 401. Foreign/missing owned resources both return 404.

## Authentication
POST `/auth/register`, POST `/auth/login`: `{email,password,displayName?}`.
Password 10–72 bytes and at least ten characters; valid email ≤254 chars; name ≤100. Register 201/login 200
`{accessToken,expiresAt,user:{id,email,displayName,plan,role}}`. Registration conflict
409 CONFLICT. Wrong login 401 UNAUTHORIZED. GET `/auth/me` returns user. Browser
stores token only in server HttpOnly cookie; proxy supplies Bearer header.

## Generation
GET `/image-capabilities`:
`{enabled,model:'character-image',aspectRatios:['1:1','3:4','4:3','16:9','9:16'],supportsReferenceImage,unavailableReason:string|null}`.
Enabled requires configured provider credentials and activated `ARK_IMAGE_MODEL`.

POST `/image-jobs`:
`{prompt,aspectRatio,referenceImageId?:UUID,idempotencyKey:UUID,visualStyle?,mood?,designFormat?}`.
Prompt 1–2000 characters, reference is an owned, unexpired uploaded image.
Optional validated enums (omission normalizes to the first value):
- `visualStyle`: `auto`, `cinematic_realistic`, `3d_animation`, `anime`, `illustration`, `clay`.
- `mood`: `neutral`, `warm`, `confident`, `mysterious`, `serious`, `energetic`.
- `designFormat`: `full_body`, `turnaround`, `portrait`.

`full_body` defaults to one frontal full-body character, relaxed neutral A-pose,
hands and feet visible, plain neutral background and soft even lighting. Selected
mood changes facial expression without introducing an obstructive action pose.
`turnaround` requests three consistent front/side/back views as a design sheet;
it is not the recommended direct image-to-video start frame. `portrait` requests
a frontal head-and-shoulders facial reference. Style `auto` respects the brief.
The server preserves the raw brief in the job `prompt`, composes the immutable
`compiledPrompt`, and snapshots normalized `characterDesign` options plus
`compilerVersion: 'character-design-v2'` for new jobs beside existing provider/model/size in
`effective_inputs`. The worker submits this stored compiled prompt, never
recompiles during retries. Legacy jobs without compiled prompt use their original
raw prompt; legacy snapshots without characterDesign normalize to defaults for
idempotency comparisons. Changing any tag with the same idempotency key returns
409. Omitted and explicit default tags are equivalent.

These presets are production-reference guidance, not a universal AI model
standard or a guarantee of anatomical or cross-scene identity consistency.
Provider/model reference and human-face restrictions still apply. Sources:
[BytePlus virtual avatar guidance](https://docs.byteplus.com/en/docs/modelark/private-virtual-avatar-library-guide-preview)
recommends frontal full-body references and frontal portrait references;
[Runway image references guidance](https://help.runwayml.com/hc/en-us/articles/40042718905875-Creating-with-Gen-4-Image-References)
recommends even light and neutral expression.
201 `{jobId,status:'queued',estimatedCredits:null,createdAt}` (repeat may return
existing state). Same owner/key with different inputs: 409 IDEMPOTENCY_CONFLICT;
concurrent user limit and configured per-user submissions/minute: 429 RATE_LIMITED.
Per-minute counting includes explicit idempotent repeats, never status polling. Unconfigured provider: 503 IMAGE_UNAVAILABLE.
Pricing unknown; no USD estimates are invented. Existing GET `/jobs/:jobId` and
GET `/jobs?page=1&limit=20` return existing JobDTO. Image `compiledPrompt` holds
the immutable server-composed character design prompt for review. For image jobs only, JobDTO adds `rawPrompt` (original brief) and `characterDesign: {visualStyle,mood,designFormat}` from the stored snapshot; legacy image snapshots without explicit tags omit `characterDesign` because their outputs were not compiled using these presets. Other job types omit these fields. Provider/model configuration stays private. Status queued → processing →
succeeded/failed, with cancelled supported for queued images. Result remains null
until validated image is durably stored; then `result.mediaType='image'`, signed
`mediaUrl`, `expiresAt`. Numerical progress for image jobs is indeterminate.
Failed jobs are retried only by explicit new submission/new idempotency key;
workers never auto-resubmit a lost potentially billed image request. Processing
leases older than ten minutes fail with WORKER_INTERRUPTED and actionable message.

## Asset Library
POST `/assets`: `{name,assetType,description?,prompt?,scope:'personal',jobId?:UUID,uploadId?:UUID}`.
Type: character/location/prop/costume/style. Name trimmed 1–100, description/prompt
≤2000. At most one jobId/uploadId; neither creates metadata draft. Saving a job
requires owned succeeded **image** output in durable storage (409
ASSET_SOURCE_NOT_READY otherwise). Saving same owner/job twice returns 409 CONFLICT.
When saving from a job, the server uses the owned job’s raw brief as the asset
prompt, irrespective of the client prompt. Send raw brief ≤2000 or omit prompt;
do not send the expanded compiledPrompt.
201 AssetDTO:
`{id,name,assetType,description,prompt,scope,projectId:null,jobId:string|null,uploadId:string|null,mediaUrl:string|null,mediaExpiresAt:string|null,status:'draft'|'ready',version:1,createdAt,updatedAt}`.
GET `/assets?type=character&search=...&page=1&limit=20`: paginated
`{items:AssetDTO[],total,page,limit}`. Limit max50; search ≤200 chars.
GET `/assets/:id`, PUT `/assets/:id` `{name?,description?}` return AssetDTO.
DELETE `/assets/:id` returns 204. UUID path validation returns 400 VALIDATION_ERROR.
Deletion soft deletes metadata only and never removes underlying job/upload media;
existing scene uploads remain intact. No cross-project/team sharing is claimed.

## Deployment and data
Apply additive migrations 014 and 015 separately, in order. Migration 014 adds user password hash/role and
library_assets with owner/source FK/indexes; existing users remain without local
password and cannot log in until an explicit account recovery/import process.
Case-insensitive duplicate existing emails must be resolved before migration.
Migration 015 adds the missing uploads.updated_at column required by the existing
Upload Base model; existing uploads and migration history are preserved.
Use a random JWT_SECRET ≥32 bytes; example placeholder is rejected. No DevAuth
fallback. Configure activated Seedream image model separately from video model.
BytePlus primary API: https://docs.byteplus.com/en/docs/ModelArk/image-generation-api
Official cached Go SDK v0.11.0 `arkruntime/images.go` exposes GenerateImages;
request b64_json prevents remote output URLs from entering library records.

## Product image category (additive; no migration)

`POST /image-jobs` additionally accepts `category?: "character" | "product"`.
Omission means `character` and preserves existing presets and legacy behavior.
For `product`, omit `designFormat` (empty is equivalent); any nonempty value,
including `full_body`, `turnaround` or `portrait`, returns 400 `VALIDATION_ERROR`.
Product framing is one complete, centered packshot on a neutral background.
The same `visualStyle` and `mood` enums apply with product-specific material,
color and lighting instructions, without character anatomy, poses or expressions.
Defaults remain `auto` and `neutral`. Existing reference-image ownership, image
MIME validation and expiry checks apply unchanged.

Product compilation instructs preservation of recognizable product/package
details and existing labels, and forbids inventing brands, logos, label text,
certifications or marketing claims. These are generation instructions, not a
guarantee of exact label fidelity; users should review generated output.

The immutable job snapshot stores `category: "product"`, normalized
`productDesign: {visualStyle,mood}`, and `compilerVersion: "product-design-v1"`
alongside existing provider/model/size. Character snapshots retain
`characterDesign` and compiler `character-design-v2` for new jobs with explicit
`category: "character"`; stored legacy `character-design-v1` snapshots remain unchanged.
Missing category in legacy snapshots means character. Workers submit the stored
compiled prompt; retrying a key never changes its prompt or snapshot.
Changing category, normalized options, brief, ratio or reference for the same
idempotency key returns 409 `IDEMPOTENCY_CONFLICT`; omitted and explicit defaults
are equivalent.

For image jobs only, JobDTO adds `imageCategory: "character" | "product"`
(legacy snapshots report character). Product jobs expose
`productDesign: {visualStyle,mood}` from their snapshot and omit `characterDesign`.
Character jobs preserve existing `characterDesign` behavior and omit
`productDesign`. Non-image jobs omit these additive fields. `rawPrompt` and
`compiledPrompt` retain their existing provenance semantics. Provider/model
configuration remains private.

Saving a product job via `POST /assets` requires existing `assetType: "prop"`
(UI label สินค้า/พร็อพ); other types return 400 `VALIDATION_ERROR` before saving.
The server retains the original job ID, durable storage key and job raw brief,
ignoring client-supplied prompt overrides. The job's compiled prompt and snapshot
remain immutable. Existing character-job and upload asset save behavior is
preserved. No database enum, schema, migration or provider endpoint changes.

### Image category capability guard

`GET /image-capabilities` additionally returns `categories: ["character", "product"]`
regardless of provider readiness. This advertises API feature support; existing
`enabled` and `unavailableReason` still describe provider readiness. Existing
`model`, `aspectRatios` and `supportsReferenceImage` behavior is unchanged. Clients
must require an explicit `product` entry before submitting a product request,
because older API binaries may ignore unknown request fields and generate a
character prompt. A missing categories field means product support is unconfirmed.

## Character reference image-to-image (2026-10-22)

`GET /image-capabilities` returns `supportsCharacterReferenceImage: boolean`,
`characterReferenceMode: "image_to_image"`, and
`characterReferenceUnavailableReason: string|null`. Character reference support
requires only a configured image generation model; an analysis model is not required.
`supportsReferenceImage` continues to describe direct image conditioning for products.
Missing image generation configuration returns HTTP 503 `IMAGE_UNAVAILABLE`.

`POST /image-jobs` and its optional uploaded `referenceImageId` are unchanged.
New character jobs freeze compiler `character-design-v2`, normalized design options,
and final `compiledPrompt` at enqueue. A supplied reference freezes private
`imageGenerationMode: image_to_image`; jobs without a reference freeze `text_to_image`.
The worker checks upload ownership and expiry, signs its storage key for ten minutes,
and sends that image directly to Seedream together with the compiled prompt.
New character jobs never invoke reference analysis and contain no analysis strategy,
model, description or version metadata. Product behavior is unchanged.

The new compiler requests a synthetic, fictional production character and retains
reference face geometry, hair silhouette, body proportions, palette, clothing and
accessories as closely as possible except where the brief explicitly changes them.
Edits apply only to their corresponding traits; style, mood and framing affect their
respective dimensions. Exact likeness and provider acceptance are not guaranteed.

Previously queued `descriptive_text_v1` jobs retain their frozen analysis model,
analysis version and text-only generation. Both `general-character-traits-v1` and
`visible-character-traits-v2` analysis versions retain their original compilers;
unknown versions still fail. Prepared descriptions and prompts replay without new
analysis. Existing jobs without strategy markers retain direct-image behavior.
Same-key replay validates original owner, inputs and options before configuration
or upload expiry, returning the immutable original job. Changed inputs return
409 `IDEMPOTENCY_CONFLICT`. No schema migration or new request field is required.

### Video realism

The existing video flow accepts an owned image as `first_frame` / `image_video`
and passes it to Seedance with the compiled video prompt. For supplied first-frame or subject-reference images, the video compiler adds
natural believable motion, coherent anatomy, physically plausible lighting/materials
and temporal stability, retaining facial structure, proportions, hair and clothing
unless explicitly changed. The selected visual style is preserved, including animation.
No global realism override is added: selected visual style and explicit creative
intent remain effective. A synthetic character label does not guarantee moderation
acceptance or make an unapproved reference eligible for use.
