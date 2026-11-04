# Video production API contract

All paths below are relative to `/open/v1`; JWT required. Existing `{data:...}` success and `{code,message,details?,requestId}` error envelopes apply. Owner mismatch is 404. All timestamps RFC3339. No provider credits are consumed by planning/edit/export APIs.

## Projects

`GET /projects?page=1&limit=20` → `{items:Project[],total,page,limit}`.
`POST /projects` body `{title?:string,brief:string,aspectRatio?:string}` → Project (201). title inferred from brief; aspectRatio defaults16:9. Project and first editable scene from brief are created in one transaction; no AI planning claim.
`GET /projects/:id` → ProjectDetail.
`PUT /projects/:id` body `{revision:number,title?:string,brief?:string,aspectRatio?:string}` → Project. Stale revision409.
`DELETE /projects/:id` → `{deleted:true}`. Soft deletion, preserves clips/assets/jobs.

Project = `{id,title,brief,aspectRatio,revision,editDraft:EditDraft,createdAt,updatedAt}`.
ProjectDetail = Project plus `{scenes:ProductionScene[],exports:Export[]}`.
EditDraft = `{clips:EditClip[],audioUploadId?:string|null,audioVolume:number,textLayers:TextLayer[]}`.
EditClip = `{sceneId:string,jobId?:string|null,trimStart:number,trimEnd:number,enabled:boolean}`. trimEnd0 means full source duration. Disabled clips are preserved in draft, omitted from export. Explicit jobId freezes selected version; null means current selected version. All requested scene/job/upload IDs must belong to owner and project.
TextLayer follows existing export input `{sceneId,text,position?,fontSize?,color?,x?,y?,align?,startTime?,endTime?}`.
`PUT /projects/:id/editor` body `{revision:number,editDraft:EditDraft}` → Project. Saves draft, increments revision; stale revision409. Saving requires valid scene references; selected jobs must be succeeded video outputs. No regeneration.

## Scenes

`GET /projects/:id/scenes` → `{items:ProductionScene[],total}` (ordered, maximum100 scenes).
`POST /projects/:id/scenes` body `{title?:string,description:string,referenceAssetId?:string|null,videoSettings?:VideoSettings}` → ProductionScene201.
`PUT /dna/scenes/:id` existing fields plus `{referenceAssetId?:string|null,videoSettings?:VideoSettings}` → ProductionScene.
`DELETE /dna/scenes/:id` existing route.
`POST /dna/scenes/:id/duplicate` → ProductionScene201.
`PUT /projects/:id/scenes/order` body `{sceneIds:string[]}` → `{items:ProductionScene[],total}`; exactly all active project scenes once, atomic.
ProductionScene is existing SceneDTO plus `{projectId:string|null,referenceAssetId:string|null,videoSettings:VideoSettings}`. Existing fields include id,title,description,sceneOrder,selectedJobId,preview,characters,tags,createdAt,updatedAt.
VideoSettings = `{model:string,mode:"text"|"image_video"|"image_image",aspectRatio:string,duration:number,quality:"standard"|"high",enhancePrompt:boolean}`. Defaults derive server registry: model violet-motion-2, mode text, project ratio, duration5, standard quality. Actual configured capabilities govern accepted settings.
`GET /dna/scenes/:id/jobs?page=1&limit=20` → existing paginated JobDTO list, only videos for this exact scene. JobDTO gains `{outputDurationMs:number|null}`.
`POST /dna/scenes/:id/generate` retains existing CreateJobRequest body; frontend sends saved settings plus `{prompt:description,idempotencyKey:UUID}`. Scene's persisted referenceAssetId is resolved directly from private library; no temporary upload copy. Image-to-video uses reference as first frame; image_image uses identity/reference-image only where supported. Library reference forces image mode; text mode with reference is rejected rather than silently ignored.
`PUT /dna/scenes/:id/select-output/:jobId` requires succeeded video output attached to exact scene. HTTP 200 returns `{data:{sceneId:string,selectedJobId:string},requestId:string}`; this is a selection acknowledgement, not a SceneDTO. Refetch the scene to obtain its updated preview. Existing scene settings and older jobs remain intact during regeneration.

## Capabilities

`GET /video-capabilities` → `{available:boolean,models:VideoModel[],export:{available:boolean,supportsTextOverlays:boolean,formats:["mp4"],resolutions:["720p","1080p"],aspectRatios:["16:9","9:16","1:1","4:3","3:4","21:9"]}}`.
VideoModel = `{id,name,providerModel,aspectRatios:string[],minDuration:number,maxDuration:number,qualities:string[],supportsImageToVideo:boolean,supportsReferenceImage:boolean,supportsAudio:boolean}`. Runtime registry is authoritative; availability means configuration present, never account activation verified. Prices absent/unknown.

## Export

`POST /exports` existing fields plus `{projectId?:string,idempotencyKey?:UUID,clips:[{sceneId,jobId?:string,trimStart?:number,trimEnd?:number}],output?:{format:"mp4",resolution:"720p"|"1080p",aspectRatio:string}}` → Export201. 1–20 distinct scenes, clips ordered as sent. projectId when present requires each scene in that project. Clip job omitted uses selected job. Server snapshots durable source keys/job IDs/durations at submission; clients cannot supply storage keys. Changed selection after submission cannot alter export. Same idempotency key+different normalized request409.
Export = existing `{exportId,status,createdAt,completedAt,error}` plus `{projectId:string|null,output:ExportOutput,outputDurationMs:number|null}`. Status queued|processing|succeeded|failed; indeterminate progress.
`GET /exports/:id`, `GET /exports?page&limit`, `GET /exports/:id/download` unchanged (download `{url,expiresAt}`). `POST /exports/:id/retry` only failed export, preserves same snapshot; → Export. Processing restart recovery only re-encodes existing sources, never calls AI.
MP4 H264 output, aspect preserved by scale/pad; original clips untouched. Optional existing audio/text are supported; transitions unavailable.

## Validation

Title1–200, brief1–10000, scene description1–2000, trims finite nonnegative and end greater than start within known duration. Reference must be owned nondeleted image asset. Asset deletion while referenced returns409. Editor revision and project metadata revision share a counter. Errors400 validation,401 auth,404 missing/foreign resource,409 conflict,422 output not ready,503 provider/export unavailable. Legacy scene/export rows remain supported.

Video submission recovery follows [the authoritative generation contract](video-generation-api-contract.md#accepted-video-submission-persistence): accepted task-ID writes use bounded independent retries, never another provider POST. Exhausted writes retain the queued application job ID; after two minutes the poller reports `SUBMISSION_INTERRUPTED` with uncertain acceptance/billing and manual recovery guidance. Process-crash recovery is not automatic.

## Reference images and scene backgrounds

For new video jobs, image assets keep their submitted order and roles. Prompt
labels `Image 1`, `Image 2`, etc. count image items in that same order; text and
video items do not consume image numbers. A `reference_image` uses subject
appearance without borrowing its source backdrop. The existing scene description
and explicit scene setting override determine the environment. If neither names
an environment, no specific replacement environment is prescribed; the server
does not invent a white/studio background.

`first_frame` and `last_frame` retain the complete supplied frame, composition and
background as the requested video start/end. They do not receive subject-only
background replacement instructions. Existing scenes keep their saved mode;
background replacement intent requires choosing reference-image mode rather than
silently changing first-frame semantics. These are prompt instructions, not
actual image segmentation or a guarantee of generated background separation.

Scene camera/setting overrides are sent even without a style profile, and scene
camera overrides retain precedence over style camera settings. Negative DNA tags
are included in the actual text content under `Constraints:`; no unsupported
provider negative-prompt field is added. Role/background controls, scene overrides
and negative constraints precede the creative brief within the existing
2,000-rune prompt budget. Normal-sized controls survive long briefs; control text
that alone exceeds the budget can still be truncated. Character DNA and
consistency notes remain part of the creative input under existing limits.

Only newly compiled submissions use these instructions. Replaying an existing
job with the same request and idempotency key returns that stored job without
changing its original compiled prompt/snapshot or submitting another provider POST.
No face-identity guarantee, model-policy bypass or new request field is introduced.
