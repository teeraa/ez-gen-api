# Film (สร้างหนัง) API — elements, per-shot reference-to-video, chat planning

Status: DRAFT (2026-11-12). Code written without a local Go toolchain: `go build`, `go vet` and
`go test` are **NOT_RUN** and must be run before use.

## Isolation from Director

- New tables only (`migrations/033_film.sql`): `film_elements`, `film_episodes`, `film_jobs`,
  `film_receipts`, `film_point_entries`. Director tables are read (series/projects, row locks) and
  the shared wallet `director_point_accounts` is debited/credited.
- New code only: `apps/models/film.go`, `apps/repositories/repository_open_api/film_repository.go`,
  `apps/facades/facade_open_api/film_{facade,prompts,worker,test}.go`,
  `apps/handlers/handler_open_api/film_handler.go`, `apps/crons/film_worker.go`,
  `apps/dto/dto_open_api/film_dto.go`, `apps/requests/request_open_api/film_requests.go`.
  Director functions are called, never changed. `main.go` gains the film facade, routes and worker.
- `film_jobs` is claimed only by `FilmWorker`; the Director worker never sees film jobs.
- Prices reuse `DIRECTOR_IMAGE_POINTS` and `DIRECTOR_VIDEO_POINTS` (price of 5 s of video).

## Concepts

- **Series** = `director_series` with `mode='film'`. Other series answer `404 FILM_NOT_FOUND`.
- **Element** = character | location | prop of a series, with a reference image. For
  `realistic`/`cinematic` series a character also needs a **faceless costume reference**
  (`synthetic_key`), made by Director's faceless casting prompt from the character image, because
  Seedance rejects realistic face references. `prepared` in responses says the element is usable.
- **Episode** = a `director_projects` row of a film series; its film document (shots + video
  state) lives in `film_episodes`. `revision` advances on client writes only.
- **Shot**: `seconds` 4–15, `transition` `cut` | `continue` (first shot must be `cut`),
  ≤4 characters, ≤1 location, ≤3 props, `action` ≤2000, `camera` ≤300, `audio` ≤600,
  ≤6 dialogue lines (speaker must be a character of the shot, line ≤300, delivery ≤120),
  ≤40 shots, total ≤600 s.

## Video modes

| transition | sent to provider | references |
|---|---|---|
| `cut` | text + `reference_image` per element (characters, location, props; ≤9) | prompt labels `Image N` = `@character`, `#location`, prop |
| `continue` | text + previous clip's last frame as `first_frame` | elements as text only (first-frame and reference modes cannot be mixed) |

`continue` waits (up to 3 h) while the previous shot's video is running and fails with
`VIDEO_DEPENDENCY_NOT_READY` if it is not succeeded. For realistic/cinematic, a rejected last frame
(`PROVIDER_IMAGE_REJECTED`) is re-sent once in reference mode. Realistic/cinematic prompts start with
a live-action lead and pass user text through Director's style-word neutralizer. Dialogue/audio set
`generate_audio` when the model supports audio. Prompts are validated against the application
video prompt budget before charging.

## Routes (`/api/v1/film`)

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/series/:id/elements` | – | `200 { items: FilmElement[] }` |
| POST | `/series/:id/elements` | `{ kind, name, spec?, assetId?, idempotencyKey }` | `201 FilmElement` (`assetId`: library asset of the same type supplies the image) |
| PATCH | `/elements/:id` | `{ revision, name?, spec? }` | `200 FilmElement`; stale revision → `409 REVISION_CONFLICT` |
| DELETE | `/elements/:id` | – | `204`; queued image job refunded |
| POST | `/elements/:id/image` | `{ mode?: "generate"\|"prepare", idempotencyKey }` | `202 FilmElement`. `generate` costs 1 image (2 for realistic/cinematic characters: portrait + faceless reference); `prepare` (realistic/cinematic character with an image) costs 1 |
| GET | `/episodes/:id` | – | `200 FilmEpisode` (shots with signed clip URLs and `stale`, elements, video models, prices, balance) |
| PUT | `/episodes/:id/shots` | `{ revision, shots: FilmShot[], idempotencyKey? }` | `200 FilmEpisode`; shots keep their video when their `id` is kept; a shot with a running video cannot change or be removed (`409 SHOT_BUSY`) |
| POST | `/episodes/:id/shots/:shot/video` | `{ modelId?, idempotencyKey }` | `202 FilmEpisode`; cost `ceil(DIRECTOR_VIDEO_POINTS × seconds / 5)`; elements not prepared → `409 ELEMENT_NOT_READY` |

Errors: `404 FILM_NOT_FOUND`, `409 IDEMPOTENCY_CONFLICT`, `409 ELEMENT_NAME_TAKEN`,
`429 RATE_LIMITED` (more than 30 active film jobs per user), `402 INSUFFICIENT_POINTS`,
`400 VALIDATION_ERROR` (same shapes as Director), `503 IMAGE_UNAVAILABLE` / `VIDEO_UNAVAILABLE`.

A clip is `stale` when its shot or any referenced element (name, spec, image, faceless reference)
changed after it was made.


## Phase 3: chat planning (2026-11-12, `migrations/034_film_plan.sql`)

`POST /episodes/:id/plan` `{ prompt (1–2000), targetSeconds? (10–300, default last value or 60), shotId?, idempotencyKey }` → `202 FilmEpisode`.
Without `shotId` the assistant plans the whole episode: story (title, logline, synopsis, theme in Thai), new elements
(Thai names, English visual specs; existing elements are reused by kind+name, at most 14 elements per series) and shots
(English action/camera/audio, Thai dialogue) whose seconds are rebalanced to exactly `targetSeconds`; the shot list is
replaced. With `shotId` only that shot is rewritten (id and clip kept; the clip becomes stale). Cost: `DIRECTOR_STORY_POINTS`
for a whole plan, a quarter (≥1) for one shot; refunded on failure. Uses the Director story planner configuration
(`DIRECTOR_STORY_PROVIDER` / Ark or Gemini model) through its own `plan` job in `film_jobs`.
Rejected with `409 PLAN_BUSY` while a plan runs and `409 SHOT_BUSY` while a video of an affected shot runs; `503 PLAN_UNAVAILABLE`
without a configured planner. LLM references that do not resolve are dropped; dialogue speakers must be in the shot;
`continue` is kept only when the location is unchanged; the first shot is always `cut`.
`FilmEpisode` gains `story`, `messages` (≤80, user/assistant), `plan` (`queued|processing|failed` or null), `targetSeconds`,
`planPoints`, `planAvailable`; `FilmElement` gains `syntheticUrl` (faceless reference of realistic/cinematic characters).
Element images now report `processing` while their job runs.

## Export (phase 4)

`POST /film/episodes/:id/export` `{idempotencyKey, subtitles?}` → 202 episode. Free (cost 0).
Queues an `export` job (migration `035_film_export.sql`) that merges every shot whose video
`status=succeeded`, in shot order, with the shared FFmpeg processor. Episodes with more than
20 clips are merged in batches of 20, then the parts are merged. With `subtitles`, each clip's
dialogue lines are burned in at the bottom. Output: `film/{user}/{project}/export/{job}.mp4`.

Episode response adds `exportAvailable` and `export {status, progress, url, expiresAt, stale,
clipCount, skipped, subtitles, failureMessage}`. `stale` is true when the set of finished clips
changed after the export was made. Errors: 409 `EXPORT_BUSY`, 409 `VIDEO_INCOMPLETE` (no
finished clip), 503 `EXPORT_UNAVAILABLE`.

## Voices and re-dubbing (2026-11-23, `migrations/041_film_redub.sql`)

Every character in a shot needs a voice: `POST .../shots/:shot/video` (draft/full) → 422 `VOICE_REQUIRED`.
A finished clip keeps the provider clip before dubbing (`rawKey`, `{job}-raw.mp4`) next to the dubbed
`clipKey`. The video hash no longer contains voices, so changing a voice does **not** make a clip stale.

Shots with dubbed lines are rendered **without provider audio** (`generate_audio` off): Seedance voices
dialogue even when told not to, which doubled the speech. Such clips have no sound effects/ambience from
the provider. Clips (and drafts) made with provider audio and dubbed lines report `voice: "overlap"` and
are `stale`; a draft like that cannot be finalized (409 `DRAFT_NOT_AVAILABLE`).

`video.voice` per shot: `current` · `outdated` (a speaker's voice changed) · `updating` · `failed`
(`voiceMessage`) · `legacy` (made before raw clips were kept, or voiced by the provider: only a new
video changes its voices). Episode: `redubShots`, `redubPoints`, `redubAvailable`.

`POST /film/episodes/:id/redub` `{idempotencyKey}` → 202 episode. Queues one `shot_redub` job per
fresh clip whose voice is `outdated`/`failed` (1 pt per started 10 s of speech, refunded on failure);
409 `NOTHING_TO_REDUB` when none; 503 `VOICE_UNAVAILABLE`. The job mixes the current voices over the
raw clip into `{job}-dub.mp4`; a new video for the shot cancels it (refunded). The export becomes
stale because the clip key changes.

## Studio edits (2026-11-23)

- Shots carry `trimIn`/`trimOut` (seconds, rounded to 0.1, at least 1 s kept) in `PUT .../shots` and
  the episode DTO. Trims never make a video stale; the export cuts each clip by them.
- Music track: `POST /film/episodes/:id/music` multipart `file` (MP3/WAV/M4A, ≤30 MB, ≤20 min) +
  `idempotencyKey` → 200 episode with `music {name, seconds, volume, url, expiresAt}` (default volume
  0.35). `PATCH` same path `{idempotencyKey, volume}` or `{idempotencyKey, remove: true}`; 409 `NO_MUSIC`.
  The export mixes it from 0 s under the whole film with a 2 s fade-out.
- `export.stale` now also follows trims and music (an episode with neither keeps the old hash).

## Dialogue as audio clips, moved lines, music start (2026-11-23)

- A clip keeps each generated dialogue line as its own MP3 (`{clip}-line-NN.mp3`). `video.rawUrl` is the
  clip without dialogue and `video.lines [{speakerId, text, url, seconds, at}]` the lines with the
  place they were generated at (seconds from the clip start). `video.url` stays the mixed clip.
- Shots carry `lineAt` (same order as `video.lines`, 0.1 s steps, ≤ clip seconds) to move lines in the
  studio. It never makes the video stale; a new video clears it; a re-dub keeps it if the count matches.
- The export mixes moved lines again over the raw clip; unmoved clips are used as they are.
- Music `PATCH {idempotencyKey, start}` places the music on the film timeline.

## Voice first and per-line emotion (2026-11-23)

- Dialogue lines carry `emotion` (a Voice-menu emotion; "" = the speaker's voice setting). The planner
  picks it per line from the shot and scene; the voice also hears the shot's action as context.
  Emotions added: ตะโกน, โกรธ, หวาดกลัว.
- Voice first (`FilmOptions.VoiceFirst`, on in main.go): before submitting a shot with dubbed lines the
  worker generates the lines, stores them and a dialogue track (`{job}-dialogue.wav`, lines at their
  times, as long as the shot) and sends it as `audio_url` role `reference_audio` ("audio 1") with
  `generate_audio=true`; the prompt asks for lip-sync to audio 1. The same stored lines are mixed over
  the returned clip (clip audio muted), so timing matches. If the provider refuses the audio reference
  (4xx, e.g. continue mode with a first frame) or the task fails, the shot is submitted once more
  without it and dubbed after the video. A finalized draft keeps its draft's lines.

## Edits by position (phase 6, 2026-11-23)

Positions are always named **"ฉาก N · ช็อต M"** (shot numbers restart in every scene; a scene starts at
the first shot and at every `cut`), in the UI, in chat messages and in what the assistant sees.

- `POST /episodes/:id/plan` on an episode **with shots** is an edit. Optional `scope: "shot"|"scene"` with
  `shotId` is the timeline selection (scene = the shot's whole scene). Before any LLM call the server
  checks the message: no selection and no "ฉาก/ช็อต/scene/shot + number" (Thai digits accepted), a
  scene that does not exist (one past the last is allowed for "add"), a shot that does not exist (one
  past the scene's last is allowed), or "ช็อต M" without a scene when there are several scenes → the
  user message and an assistant question are stored, **no job, no charge** (200/202 episode, `plan` null).
  A whole-film request ("ทำให้ทั้งเรื่องเศร้าขึ้น") is always asked back. Planning an empty episode is unchanged.
- The job (`mode: "ops"` in its snapshot) asks for operations instead of a shot list:
  `edit {scene, shot, data}`, `delete {scene, shot?}` (no shot = whole scene),
  `add {scene, shot, data}` (becomes that position; shot = count+1 appends to the scene),
  `add {scene, newScene: true, data}` (new scene in front of scene N; N = count+1 appends),
  `voice {character, voice}` (preset id). All positions use the numbering before the edit. An ambiguous
  request returns `question` and no ops. Every position is checked; an unknown position or character, or
  a question, ends the job with an assistant message and a **refund**.
- Cost: a selected shot with no other position in the message = a quarter of `planPoints` (≥1); anything
  else `planPoints`. An edit's `data` is laid over the current shot (fields left out stay); seconds are clamped to 4–15.
- Edited shots keep their id (the clip becomes stale); trims and moved lines reset. A shot `continue`s
  exactly when the shot before it belongs to the same scene (stored scenes keep their shots; added shots
  name theirs) and, for added/edited/re-joined shots, stands in the same location. Adds in front of one
  shot are ordered: end of the previous scene, new scenes, then shots of that shot's own scene.
- **Confirmation**: if the edit deletes a shot with a finished clip or a draft, makes another paid clip
  out of date (new transition or new previous shot: `outdated[]`), or a voice change costs points, nothing
  changes yet: the episode gets `pending {id, reply, summary, deletes[], outdated[], voices[{name, voice,
  redubClips, remakeClips}], redubPoints, videoPoints, stale, createdAt}`. `redubPoints` covers every
  out-of-date clip of the episode (what `/redub` does).
  `POST /episodes/:id/plan/confirm {idempotencyKey, pendingId, accept}` → 200 episode. The shot layout
  (ids + transitions) must be the one the card was made on, and no newly paid clip may be deleted, else
  `409 PLAN_STALE`. A voice change is done first (402 `INSUFFICIENT_POINTS` checked up front): set the
  voice, run `/redub`, and queue a new video (same model, draft or full as before) for clips whose voice is
  part of the video (`legacy`/`overlap`; the list is fixed when the card is made, on the edited shots) —
  decided 2026-11-23; the shot edit is re-checked before anything is charged;
  jobs use keys derived from the confirmation key. Only then is the edit applied and the card removed, so
  a failure leaves the card and a retry finishes. `stale` counts continuation clips that then start on a
  changed frame. A new chat message or `PUT /shots` drops the pending edit. A voice change that costs
  nothing is applied right after the job. Elements the assistant created stay even if the card is cancelled.
- `messages[].target` and `plan.target` carry the position label shown at the time.

## Cinematic look for realistic/cinematic (2026-11-23)

- Video prompt lead (`filmFilmedLead`): feature-film footage on prime lenses, shallow depth of field,
  motivated light, muted grade with lifted blacks, 35mm grain, natural skin, restrained acting; reference
  images give identity/costume/place only (not pose, framing, backdrop or flat light). Filmed prompts end
  with `filmFilmedAvoid` (centered symmetry, staring into the lens, HDR gloss, plastic skin, teal-orange,
  flat light, deep focus, CG look, frozen poses).
- Planner (`filmCinemaRules`, filmed only): every camera names size, lens, focus and one move; every
  action opens with composition (third, soft foreground, off-lens eyeline, motivated light) and a small
  physical task; dialogue covered in singles/OTS/inserts; ordinary lived-in sets and wardrobe, no
  generated clichés; trailers use 4–5 s shots; audio always names room tone.
- Filmed element images: location = film still from eye height, 35mm, practical light, grain (was an
  empty establishing plate); character = costume photo in side window light without retouching.
  Existing elements keep their images until regenerated.
- Dub mix version 2: clip audio stays muted under dubbed lines but a deterministic brown-noise room tone
  at about -50 dBFS runs under the whole clip, so dubbed shots never fall to digital silence. Mixes
  before version 2 are redone from the raw clip on export; `overlap` still keys on version ≥ 1.

## One voice per character, lines that fit, camera angles (2026-11-23)

- Voice anchor: described voices (presets, designs) are sampled anew on every call, so a character's
  voice drifted between shots (measured F0 101/162/140 Hz for one character). Each character now gets
  an anchor clip once (neutral sample, `film/{user}/voices/{element}-{identity}.mp3`, identity = voice
  without mood) and every line is spoken with it as `@Audio1` (`VoiceFacade.SpeakLineAs`); the line's
  emotion and speed still apply. Line hashes are unchanged, so old clips are not offered a re-dub
  (a re-dub would break the lip-sync of voice-first clips).
- Lines fit the shot: if the spoken lines end after `seconds - 0.3`, they are spoken again up to 1.35x
  with a 0.15 s lead and 0.1 s gaps (both voice-first and dub-after), so the dialogue track, the
  lip-sync reference and the clip end together. The planner keeps a shot's lines to about
  (seconds - 1) x 8 Thai characters.
- Planner camera angle: never square-on; 30–60° off the face line, varied height, two people never
  both facing the lens. Character specs open with nationality, age and a precise face plus one mark.
  Filmed prompts also avoid "head-on frontal camera angles".

## Film skills (2026-11-24, `migrations/042_film_skills.sql`)

Skills-menu skills with `output: "film"` carry a `directive`: English guidance for the film planner
(lens, light, composition, coverage, audio). Nine built-ins are seeded (action, noir, romance,
symmetrical pastel, documentary handheld, horror, epic opening, dialogue drama, naturalistic tech drama); users fork or create
their own in the Skills menu.

| Method | Path | Body | Response |
|---|---|---|---|
| PUT | `/episodes/:id/skills` | `{ skillIds: string[], idempotencyKey }` (at most 3; `[]` clears) | `200 FilmEpisode` |

- The episode document keeps a snapshot (`id, name, icon, description, directive`) of each chosen
  skill, so the episode is unaffected if the skill is later edited or deleted; choosing it again
  refreshes the snapshot. `FilmEpisode.skills` lists them (without the directive) and
  `skillsAvailable` says whether the route is wired.
- Every plan job (whole episode, one shot, position edits) freezes the chosen directives into its
  snapshot and appends a "Director skills" section after the rules. A skill overrides the default
  composition and cliché guidance, never the technical rules (one movement per shot, continuity,
  seconds, cast, dialogue length).
- Errors: `404 SKILL_NOT_FOUND`, `422 SKILL_NOT_FILM`, `422 VALIDATION_ERROR` (more than 3),
  `503 SKILLS_UNAVAILABLE`.

## Speech modes, blocking, no silent fallbacks (2026-11-24)

- `POST /episodes/:id/shots/:shot/video` takes `speech`: `"native"` (the video model speaks the Thai
  lines itself; each speaker's generated line is sent as a voice-timbre sample; lips match; the clip
  cannot be re-dubbed) or `"dub"`/`""` (lines generated first and mixed over the clip). The web sends
  `native` by default. `FilmShotVideo.speech` reports `native` / `dub` / `""`.
- No silent fallback: a refused voice reference fails the job (refunded) with
  `AUDIO_REFERENCE_REFUSED`; a provider task failure with voice audio is retried once unchanged, then
  reported. A `429` at submission is retried for up to 30 minutes. `speech: "native"` when native
  speech is unavailable answers `409 NATIVE_SPEECH_UNAVAILABLE`.
- Shots carry `blocking` (English: frame side, posture, facing of each visible character). The planner
  writes it for every shot and keeps one side of the 180-degree line per scene; a planned shot without
  blocking inherits the previous shot's in the same scene. The video prompt states it near the top as
  `[Blocking]` and closes with `[Continuity]`. Shots without blocking keep their old hash.

## Crashed jobs stop instead of looping (2026-11-25, `migrations/043_film_job_attempts.sql`)

- `film_jobs.attempts` counts **consecutive** crashes: a claim of a job still in `submitting` whose
  lease ran out (the worker died or hung) adds 1; `Wait` (the job parks itself to poll) resets it to 0,
  so a long video poll survives the odd restart. `last_error` keeps the latest cause (≤2000 chars).
- `Tick` recovers a panic in a job: logs the stack, records `panic: …` as `last_error` and releases
  the lease for `30 s × attempt` (`Release`), so the job is retried soon instead of after 12 minutes.
  A panic no longer takes the API process down.
- At `attempts > 3` the job is stopped through the normal failure path of its kind with a refund and
  `JOB_ABANDONED` / "ระบบทำงานนี้ไม่สำเร็จหลายครั้งติดกันจึงหยุดไว้ กรุณาลองใหม่" on the element, shot,
  plan or export. If stopping also crashed (`attempts > 4`), the job is ended `failed` with a refund
  without touching its target.
- No API shape changes. Ordinary returned errors keep the old behaviour (retried after the lease).

## Fair claim, queue indexes, cleanup (2026-11-25, `migrations/044_film_queue_indexes.sql`)

- `Claim` ranks the 256 oldest runnable jobs by how many jobs their owner has running right now
  (`submitting` with a live lease), then by age, and locks one with `FOR UPDATE OF j SKIP LOCKED`.
  A user with a long queue no longer holds every worker: another user's new job goes next. The
  outer `WHERE` repeats the runnable condition so a row claimed meanwhile is re-checked and skipped.
- Indexes: `film_jobs_claim` is replaced by partial `film_jobs_queued (created_at, id)` and
  `film_jobs_leased (lease_until)`; `film_jobs_user_active (user_id, status)` serves the 30-active-jobs
  check and the running count; `film_jobs_finished` and `film_receipts_created` serve the cleanup.
  The migration builds them inside a transaction (not `CONCURRENTLY`); `film_jobs` is small enough.
- Cleanup (in `FilmWorker`, about 1–2 min after boot, then hourly with jitter): deletes
  `film_receipts` older than 30 days and sets `snapshot = '{}'` on jobs that finished more than 30 days
  ago, 1000 rows per statement. Job rows and point entries stay for the ledger. Finished jobs are
  only read again for their cost on a refund.
- Checked on PostgreSQL 16: migrations 029–044 apply; the claim picks the idle user's job, then the
  oldest on a tie, skips a row locked by another session, and uses the new partial indexes.
  `apps/tests/film_queue_integration_test.go` covers the same against `DIRECTOR_TEST_DATABASE_URL`.

## Export audio master (2026-11-25)

- After the merge, `FilmOptions.Master` (`videoprocessing.MasterAudio`, on in main.go) rewrites the
  film's audio; the video stream is copied, not re-encoded. The music bed is no longer mixed flat by
  the merge but in this pass, **ducked under dialogue**: a sidechain compressor keyed on the film's own
  audio in the speech band (300–3400 Hz; threshold 0.02, ratio 20, attack 20 ms, release 400 ms) pulls
  the music down about 12 dB under a line and lets it back between lines. Room tone (≈ -50 dBFS) does
  not trigger it; provider ambience/effects in native-speech clips can.
- The whole mix is normalized in two `loudnorm` passes to **-14 LUFS integrated, true peak ≤ -1.5 dBTP,
  LRA 11** (linear gain when the peak allows it), resampled to 48 kHz, AAC 192 kb/s. A silent film is
  left at its level. Export progress shows 80 % during the master.
- Errors: a master failure fails the export ("ปรับระดับเสียงภาพยนตร์ไม่สำเร็จ กรุณาลองใหม่").
  With `Master` nil the export behaves as before (music mixed flat, no normalization).
- Exports made before this change are not marked stale; a new export gets the master.
- `videoprocessing` tests (real FFmpeg): -14 ±1 LUFS with and without music, ≥ 8 dB duck under a
  line (12 dB measured), music placed at its start, video copied, silence untouched.

## Timed Thai subtitles and subtitle files (2026-11-25)

- `POST /film/episodes/:id/export` snapshots each clip's dialogue as `captions` (`burn` = the
  request's `subtitles`). Dubbed clips use the generated lines at their places (moved `lineAt`
  included) for exactly as long as they are spoken, cut by the clip's trims; native-speech clips and
  clips without kept lines spread their dialogue over the clip by length after a 0.3 s lead-in.
- `apps/services/subtitles` (stdlib) follows the Netflix Thai Timed Text Style Guide: ≤ 35 characters
  a line counting neither tone marks nor above/below vowels, ≤ 2 lines a cue (longer lines become
  several cues sharing the spoken time by length), ≥ 5/6 s on screen when there is room, two frames
  between cues. Lines break at spaces, inside a long Thai run before a leading vowel (เ แ โ ใ ไ),
  after ๆ ฯ ะ ำ, never before a mark or after a leading vowel. ASS braces/backslashes are neutralized.
- Every export with dialogue stores `{job}.srt` and `{job}.vtt` next to `{job}.mp4`;
  `FilmExport.srtUrl` / `vttUrl` (signed, 1 h) are null otherwise. The web offers SRT/VTT downloads,
  and its checkbox now reads "ฝังซับไตเติลในภาพ".
- Burned subtitles: `MergeRequest.Subtitles` (an .srt) is drawn by FFmpeg's libass `subtitles`
  filter after the finish and image layers, in bundled Kanit, white with outline and shadow, centred
  at the bottom (`subtitleStyle`). Batched exports burn them once in the merge of the batches. The old
  per-clip drawtext subtitles (`filmTextLayers`) are gone; export jobs queued before this change are
  read as untimed captions and burned. FFmpeg without libass fails a burned export
  (`subtitles unavailable`); the Alpine `ffmpeg` package in the Dockerfile includes libass.
- Checked: `subtitles` tests and the burn test (`TestMergeBurnsThaiSubtitlesOnTheMergedTimeline`)
  run green on Go 1.24 + FFmpeg 6.1; `filmCues` was run against the real package.

## Locked-off camera skill (2026-11-25, `migrations/045_film_skill_locked_camera.sql`)

- Built-in film skill "กล้องนิ่ง ไม่ดัน ไม่ซูม" (icon `camera`): every shot's camera starts with
  "Static locked-off" (size, angle, lens only); no push in, pull out, zoom, dolly, pan, tilt, tracking,
  crane, orbit or handheld. It overrides camera moves of other chosen skills (a push in becomes a cut
  to a tighter static shot); rhythm comes from cutting between fixed set-ups.
- Video prompts: a shot whose camera mentions static / locked-off / tripod gets `filmLockedCamera`
  (framing fixed from first to last frame) right after the SHOT lead (live-action) or the Camera line
  (other styles), whether or not the skill is chosen. Shot hashes do not change.

## Multi-shot clips, fight and dialogue rules, wuxia trailer skill (2026-11-25, `migrations/046_film_skill_wuxia_trailer.sql`)

Why: a reference theatrical trailer cuts 65 times in 124 s (median shot 1.4 s); our first trailer had
4 shots of 6-9 s because the whole-plan instruction asked for "about (target+4)/8 shots of 5-12 s".
Fights were one long take (a 5 s held sword-lock, fighters sliding together), a line was whispered with
the head bowed in a wide shot, and the provider invented synthetic clangs.

- Shots carry `cuts` (0 or 2-8 `{camera ≤200, action ≤500}`) in the document, `PUT .../shots`, the DTO,
  plan and ops JSON. A shot with cuts is a multi-shot clip: the video prompt opens with
  `MULTI-SHOT SEQUENCE` (Shot 1, Shot 2 (hard cut), ...; real speed, cut mid-move, static cuts locked
  one by one) instead of the single-camera `SHOT` lead. Cuts enter the clip hash only when present.
  `400 VALIDATION_ERROR` field `shots.cuts` for 1 cut, more than 8, or an empty action.
- Planner rules (all styles): multi-shot clips for trailers/fights/chases/montages (3-6 cuts of 1.5-3 s,
  sound inline in <angle brackets>, no dialogue); fights as exchanges, one move per cut with weight, no
  held sword-locks, slow motion, sliding fighters or standoffs over 2 s; spoken lines in their own
  medium close-up, face three-quarter, head up, clear firm voice (never whispered/breathy/head bowed);
  real dry foley, never synthetic clangs. Nationality follows the setting (wuxia: Chinese). The whole-plan
  task allows fewer, longer multi-shot clips for trailers, fights and montages.
- Filmed prompts also avoid slow-motion/floaty fighting, held sword-locks, sliding fighters, poses held
  before a cut and synthetic clangs.
- Built-in skill "ตัวอย่างหนังกำลังภายใน (ตัดเร็วแบบหนังโรง)" (icon `clapperboard`): three acts plus a
  button, multi-shot clips, fight coverage, short lines in close-ups, real foley and no music inside clips.
- Web: shot editor lists and edits a clip's cuts; saving keeps them.

## Not yet built

TTS/music mixing, integration tests against PostgreSQL.

## Required checks (owner machine)

```
go build ./... && go vet ./apps/...
go test ./apps/facades/facade_open_api/ -run 'Film|DirectorSeries'
go test -race ./...            # full gate
go run ./migrate               # applies pending migrations up to 043 (or psql -f each file)
```
