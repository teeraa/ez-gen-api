# Video editor AI: phase 2

This additive domain uses `/open/v1/video-editor` and the same authenticated response envelopes and ownership rules as [phase 1](video-editor-api-contract.md). Production is disabled by default. The implemented provider is Gemini `gemini-3.5-flash-lite`; transcription is local faster-whisper 1.2.1 by default, with an explicitly selected local Typhoon ASR Real-time ONNX CPU option (`EDITOR_ASR_PROVIDER=typhoon`). See [Typhoon setup and timing limits](../../scripts/typhoon/README.md). No provider SDK or paid fallback is used. Missing configuration never generates fixture results.

## Routes

| Method | Path | Body / result |
| --- | --- | --- |
| GET | `/ai-capabilities` | Availability, separate ASR/analysis flags, limits, cloud notice and supported options |
| POST | `/projects/:id/ai-jobs` | `{requestId,revision,assetIds,brief,targetDuration,style,audioMode,removeSilence,autoCompose?,cloudConsent:true}` → job 202 / replay 200 |
| GET | `/projects/:id/ai-jobs` | `{items: Job[]}`, newest 20 |
| GET | `/ai-jobs/:id` | Job |
| POST | `/ai-jobs/:id/select-concept` | `{conceptId}` → job; same choice replays, different choice conflicts |
| PUT | `/ai-jobs/:id/draft` | `{draftRevision,segments}` → job with incremented draft revision |
| POST | `/ai-jobs/:id/apply` | `{requestId,revision,draftRevision,approvedSegmentIds}` → phase-1 project |
| POST | `/ai-jobs/:id/cancel` | Job; fences results and cancels active work |
| POST | `/ai-jobs/:id/retry` | `{requestId}` → job 202; explicit retry only |

Capabilities: `{autoCompose:true,available,unavailableReason,transcriptionAvailable,analysisAvailable,maxAssets:8,maxSourceDuration:600,maxFrames:24,maxTargetDuration:120,maxSegments:20,maxJobsPerDay:20,maxCloudCalls:6,cloudNotice,connectionMode,supportedOptions}`. `connectionMode` is `configured` in production and `fixture` only in the isolated executable. `supportedOptions.removeSilence`, `subtitles` and `music` are true: auto mode composes grounded captions and may use existing owned audio. Filler/shaky remain false. These flags do not advertise voice or music generation. Availability requires the enable flag, installed schema, local ASR dependency/model, configured Gemini key/version, storage and FFmpeg. Default-off project job lists return empty after checking project ownership without accessing the new AI tables. Other AI actions return `AI_UNAVAILABLE` while explicitly disabled. When enabled but cloud keys are missing, existing review/draft/apply/history remain usable without cloud calls.

Job: `{autoCompose,id,projectId,sourceRevision,phase,status,stage,assetIds,brief,targetDuration,style,audioMode,removeSilence,progress:{completedAssets,totalAssets},transcripts,visuals,concepts,selectedConceptId,draft,failureCode,failureMessage,canRetry,retryAt,cloudCallsUsed,cloudCallsLimit,createdAt,updatedAt,appliedRevision}`. Arrays are nonnull. Selection/draft/retryAt/appliedRevision are nullable. Times are UTC RFC3339. Phase is `analysis|planning`; status is `queued|processing|awaiting_concept|review|applied|failed|cancelled`. Stage is `queued|transcribing|visual_analysis|concepts|planning|review|applied|failed|cancelled`. Progress reports actual completed source assets, not a fabricated percentage.

Transcript: `{assetId,language,text,hasSpeech,segments:[{start,end,text,words:[{start,end,text}]}]}`. Silent/non-speech sources have empty text/segments and `hasSpeech:false`. Word tokens may be zero-duration and may be Thai characters rather than lexical words. Typhoon ONNX uses separately decoded PCM contexts split at conservative sustained quiet pauses, with retained contexts bounded to 12 seconds and `words: []`; model emission times are not treated as phonetic word boundaries. For continuous retained audio, boundaries use the quietest measured 50ms interval sampled every 20ms between approximately 8 and 12 seconds. Each actual PCM span is independently transcribed and no samples are dropped at these subdivisions. This yields shorter editorial choices; adjacent contexts and longer runs remain available, and no multiple-cut quota is imposed. A measured energy minimum can still fall within speech. Silence inside these windows is retained. Original context boundaries are preserved when they fit the target; they are not guaranteed sentence or word-aligned cuts. Speech segment durations are positive. This feature does not claim forced-alignment or Thai word-boundary accuracy.

Visual: `{assetId,summary,frames:[{time,description}]}`. Three newly extracted JPEGs per source, maximum 512px; the tiny phase-1 timeline thumbnails are not the analysis evidence. Concept: `{id,title,summary,duration,durationReason?}`, 2–3 concepts with server IDs. Draft: `{revision,duration,durationReason?,textLayers?:TextLayer[],audioClips?:AudioClip[],compositionNotes?:string[],segments:[{id,assetId,start,end,volume,title,reason,evidenceId}]}`. Generated segments select only server-issued evidence windows; model output cannot supply arbitrary paths, asset IDs or timestamps. Each concept is an independent complete alternative aiming for the requested target, not a fraction of a shared duration budget. Planning aims for the requested target even if the advisory concept duration is shorter. Evidence no longer has a six-second shot ceiling. Speech sources in both silence modes supply original speech/pause-context units, short two-unit and medium three-unit runs, plus an optional longer continuity fallback within the requested target. Speech evidence is capped at 48 choices across sources and 24 per source; sampled starting boundaries retain choices from later source positions. Half the per-source choice budget is reserved for individual contexts (up to 12 for one source), with remaining capacity offering pairs and longer continuity alternatives. Non-speech choices remain limited to 12 sampled starts plus a tail. Remove-silence mode does not bridge gaps greater than 0.35 seconds; keep mode may retain the original gaps. Coarse Typhoon contexts are not relabeled as sentences. A source context longer than the entire target supplies a clearly marked partial-context fallback bounded by the source; it does not claim word/sentence alignment. Non-speech sources supply target-length continuous choices and a tail choice.  Refined source context descriptions are truncated for prompt size, with full transcript context supplied separately. Original candidate IDs/timestamps remain server-owned. Alternative candidates can overlap, but selected windows must not overlap source footage. Exactly adjacent selected windows from the same asset are merged only when volume, title and reason are identical. Different narrative roles stay separate for review; separate rows over identical continuous footage do not themselves create a visual change. A merged result retains a new server-issued evidence record for the exact verified union. Source visual summaries are supplied once per source to planning. Generated plans may be shorter than the requested target; there is no minimum target percentage. They must remain nonempty, use valid unique evidence IDs and owned source bounds, contain at most 20 segments, and total at most `targetDuration + 0.05` seconds (numeric tolerance) and 1200 seconds. The provider planning schema restricts evidence IDs to the actual server choices and bounds the maximum selection count by the sum of the shortest candidate durations within the target tolerance. This is a safe upper bound, not a required count; final overlap, metadata and duration validation still apply. Planning includes explicit candidate durations. Invalid planning results produce server-only static validation categories without recording transcripts or provider payloads. Preflight validates evidence IDs/source bounds and rejects empty evidence, but never rejects solely because available footage is shorter than the target. No repeated evidence, invented filler, automatic retry or automatic regeneration is added. Legacy unedited review drafts (revision 1) may also be applied when shorter, without a manual save. Historical `AI_INSUFFICIENT_EVIDENCE` and `AI_DURATION_MISMATCH` failures allow the existing explicit user retry subject to normal attempt, provider availability and budget limits; no provider calls run automatically. Applied request replays preserve the existing response. Manual edits retain known segment IDs and validate owned source/time bounds and phase-1 limits. Editing the draft invalidates client approvals.

## Concurrency, idempotency and cost limits

At most 8 ready owned sources / 600 seconds, 24 JPEGs / 3MiB, compact prompt 96KiB, response 2MiB / 8192 output tokens, 20,000 transcript tokens, target 1–120 seconds. Phase-1 timeline remains at most 20 segments / 1200 seconds. Normal cold analysis with speech uses 3 calls (source-only transcript refinement + visuals + concepts), followed by 1 planning call. All-silent analysis skips refinement. Every HTTP attempt, including 429/timeouts/unknown responses, reserves a persisted call before sending; hard maximum 6. There is no automatic HTTP retry. Explicit retry honors retryAt, the remaining budget and at most 3 claims per phase. One queued/processing job per user, at most 5 unfinished per project and 20 new jobs per UTC day. User row locks serialize quota checks and a partial unique index protects active jobs.

Creation replay is checked before revision/quota/provider checks. Selection freezes the first concept and exact duplicates cannot trigger another call. Retry request IDs are retained across attempts. Apply checks both draft and project revision and the complete, duplicate-free approval set in a project→job transaction. Exact request replay returns the original applied project metadata/segments/revision, signed URLs freshly generated; a new request ID cannot apply the job twice. Snapshots/cache never persist signed URLs/base64 images. Every worker stage, cache publication and budget mutation requires the active claim token. Expired leases become failed; they are never automatically requeued to make another paid call. DB cancellation is observed by other workers within a bounded polling interval, cancelling subprocess/HTTP contexts. An already sent cloud request may still count toward provider usage.

Source caches are owner/asset/content hash/kind/model-version/options scoped and contain only validated transcripts or source-only visual descriptions. Brief-dependent concepts/plans are never source-only cached. Operator-provided Gemini/Whisper version values must change when deploying different model/prompt/runtime generations.

409: `AI_BUSY`, `AI_SELECTION_CONFLICT`, `AI_DRAFT_CONFLICT`, `AI_ALREADY_APPLIED`, `AI_RETRY_NOT_READY`, plus phase-1 revision/idempotency conflicts. 429: `AI_DAILY_LIMIT`; a provider quota failure is persisted as `AI_RATE_LIMITED` with retryAt. 422: `AI_INPUT_TOO_LARGE`, `AI_INVALID_RESULT`; historical jobs may retain `AI_DURATION_MISMATCH` or `AI_INSUFFICIENT_EVIDENCE`. 503: `AI_UNAVAILABLE`, `ASR_UNAVAILABLE`; worker provider failures use `AI_PROVIDER_UNAVAILABLE`. Raw provider errors, API keys, transcripts/prompts and storage paths are not returned in errors/logs.

## Setup and verification boundary

See [local runtime setup](../../scripts/whisper/README.md). Apply migration `022_video_editor_ai.sql` after `021_video_editor.sql` in the target environment as a separate deployment step. No AutoMigrate runs in production. The existing Alpine API image does not contain Python/CT2; host a supported Python 3.11+ environment explicitly before enabling AI.

`cmd/video-editor-ai-fixture` refuses every DSN except the isolated `127.0.0.1:15458/ai_editor_phase2`. It supplies real handlers/PostgreSQL/storage/FFmpeg and a local Gemini HTTP fixture. Set `VIDEO_EDITOR_FIXTURE_REAL_ASR=1` with the local Whisper settings to run real ASR. The default fake ASR and `/fixture/ai-control` exist only in this executable. Auth tokens are test-only `video-editor-fixture` / `video-editor-other`. Control payload `{assetId,mode:"quota"|"invalid"|"slow"|"normal",remaining:1}` scopes faults to an asset; GET returns call counters. No real Gemini quality/accuracy/free-tier availability is inferred from fixtures. Live verification requires a securely configured key and suitable test footage.

Provider references: [model](https://ai.google.dev/gemini-api/docs/models/gemini-3.5-flash-lite), [structured REST](https://ai.google.dev/gemini-api/docs/generate-content/structured-output), [faster-whisper](https://github.com/SYSTRAN/faster-whisper), [Google unpaid-service data terms](https://ai.google.dev/gemini-api/terms#unpaid-services). API free quota is account-dependent; a configured billing-enabled account may bill. No automatic fallback is used.

The internal analysis prompt signature is `editor-prompts-v3`. New jobs use the updated prompts and vision cache namespace; ASR cache signatures are unchanged. Existing completed jobs are not regenerated, and no automatic paid reanalysis is triggered.

Existing-job status GET reads the owned job directly without schema introspection or ASR/provider availability checks. Explicitly disabled AI remains `503 AI_UNAVAILABLE`. A missing AI job table (`42P01`) returns `503 AI_UNAVAILABLE`; other database read failures return `503 AI_STATUS_UNAVAILABLE` with a temporary status-read message, without suggesting ASR configuration changes. A worker's cancellation watcher distinguishes database observation failures from a confirmed lost claim: failures back off and retry within the existing 15-minute work deadline; confirmed cancellation still cancels the local subprocess. Persisted failure codes are historical job outcomes and are separate from a later polling error.

## Transcript refinement

After local ASR, raw transcripts are persisted while `stage:transcribing` remains active. A source-only Gemini call conservatively improves punctuation, spacing and unambiguous ASR wording before visual analysis, concepts or planning. The refined transcripts are persisted before the stage changes to `visual_analysis`, so polling can display them immediately without waiting for concepts. No database stage or column is added.

Each speech transcript may add `refinement:{text,segments:[{segmentIndex,text,uncertain}]}`. Original `text`, speech segments, words and all timestamps stay unchanged. The server validates one unique zero-based index per original speech segment, orders the results, and constructs `refinement.text`. Uncertain segments always retain the entire original segment text, even when the model suggests a replacement. The interpretation is not verified audio and does not modify the audio. The existing provider signature remains compatible; the separate refinement-v1 cache namespace versions this new step without invalidating existing planning jobs. Existing jobs omit refinement and continue to display/use raw speech.

Concepts use refined text when present. Planning receives source-level refined transcript context in every audio/silence mode. Speech evidence uses refined segment interpretation, preserving original speech/word boundaries; split-window descriptions explicitly identify the text as full-segment context rather than word-level alignment. Source-only refinements are cached under existing kind `asr` with a separate `refinement:` signature derived from both ASR and provider generations, never overwriting raw ASR records. Source/owner/content scoping and claim/cancel fences remain enforced. Invalid or oversized refinement fails explicitly through existing errors; it does not invent a fallback result, bypass the 6-call limit, or automatically regenerate. This extra call can add latency and provider usage; actual semantic quality requires evaluation with real Thai footage, not the isolated fixture.

## Target duration and nonblocking notices

`job.targetDuration` remains the user's requested target; each concept's `duration` is its planned estimate, and `draft.duration` is computed from actual selected windows. Concepts and drafts optionally include `durationReason` (Thai string; empty/omitted when reaching 95 percent of target). Existing jobs without this field remain valid. A model's unexplained short concept estimate is raised toward the requested target, capped by unique candidate coverage; an explicit shorter story with a reason remains selectable. Prompts receive candidate durations and aim for the full target rather than dividing the duration among concepts.

For shorter results, the server distinguishes limited prepared candidate coverage from a model-selected shorter interpretation. The fallback does not assert that all original source content is insufficient. Generated drafts report the actual duration and explanation; manual saves recalculate the notice and clear stale reasons when reaching target. A shortfall never creates a failed status, automatic provider retry, new paid call, or a minimum-duration approval/apply gate. Users may accept the shorter result, manually edit it, or start a new analysis. Existing job evidence and histories are not regenerated automatically. No database column or migration is required because the optional notice lives in the existing concept/draft JSON.


## Editorial pacing and pause-cache generation

Concepts and planning use source-supported opening, development, key point and ending as editorial roles. Roughly 3–6 units for 60 seconds is a soft guide, never a minimum-count failure or a fixed shot duration. A justified long shot remains valid when a useful change is not supported. Prompts no longer prefer the longest candidate or the fewest runs; they ask for meaningful selects, preservation of necessary context, and no invented visual angles or B-roll. Existing duration notices, target ceilings and nonblocking shorter approval remain unchanged.

Typhoon retains its stored provider `Signature()` for queued/planning compatibility and exposes a separate `CacheSignature()` with `pause-segments-v2`. Raw ASR and derived refinement caches use this separate identity. New analyses do not reuse old coarse-context or v1 ASR/refinements, while completed job transcripts and legacy planning remain unchanged. Other transcribers keep their existing cache signature. No migration, provider call or automatic regeneration is triggered by this cache separation.

## Automatic highlight composition

`autoCompose` is an optional boolean on creation, omitted/false preserves the
existing concept selection and segment approval flow. New clients may default it
to true only when capabilities advertise `autoCompose:true`. The frozen input
stores this choice, ready owned project audio sources and the presence/count of
manual tracks; no audio URL or arbitrary external source is accepted. Analysis
ranks source-grounded alternatives and atomically queues planning for the first
recommended concept. It does not pause at `awaiting_concept`. Planning still uses
only server evidence IDs; the provider schema never accepts arbitrary timestamps,
paths, caption timing or music URLs. The same cloud budget and claim fences apply.

The server expands a valid cut plan into `draft.textLayers`, `draft.audioClips`
and Thai `draft.compositionNotes`. The first two use the phase-one track shapes,
including optional `originJobId` UUID metadata. These draft fields are optional on the wire: empty arrays/notes are omitted,
including legacy jobs without composition. Clients normalize omitted fields to `[]`. Text combines short
plan-derived source-grounded opening/keypoint titles and original ASR captions.
A coarse ASR segment is captioned only when its entire timed span lies inside the
cut; partial segments require wholly contained timestamped words. Caption words
are grouped into short windows and mapped from source time to concatenated edit
time. Full context or refined interpretation is never assigned to an unsupported
shorter window. Segments containing zero-duration tokens use the original whole-segment text
only when fully contained; they are not partially captioned or stripped of Thai characters. Long coarse text without safe
subdivision is omitted with an honest no-caption note when none remain. Layer
text uses at most six short lines, with the existing 500-character and 50-layer
limits; room is reserved for frozen manual text layers.

Original audio follows `audioMode` (default client choice `keep`). If an owned
ready audio asset exists and there is no manual audio, its first bounded source
range is used as a quiet 12% bed; a short asset is not repeated. This does not
classify an uploaded file as music or synthesize speech. Without such an asset,
only original sound remains and the draft explains the absence of a background
bed. Existing manual audio suppresses the bed. There is no generated voiceover,
TTS or music-provider call. Subtitles still require renderer text capability to
export; a missing drawtext/font capability is reported by the existing editor.

Draft reorder/trim/delete regenerates composition against the frozen sources and
original transcripts without a cloud call. Apply keeps the existing explicit
`approvedSegmentIds`, revision guard, draft revision and idempotency contract;
clients may approve the entire automatic edit in one action. In one transaction,
apply replaces video segments and prior tracks explicitly marked with a valid
`originJobId`, preserving every unmarked manual track. Current manual audio also
suppresses a generated bed even if added after analysis. Current project audio
ownership/ready/source bounds, text validity, timeline bounds and merged counts
are validated before apply; overflow fails instead of deleting manual layers.
Manual-save normalization accepts only omitted or valid UUID origin metadata.
Legacy/manual-mode jobs continue replacing segments alone. No migration or
regeneration of existing jobs is required.
