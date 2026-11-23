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

## Not yet built

TTS/music mixing, integration tests against PostgreSQL.

## Required checks (owner machine)

```
go build ./... && go vet ./apps/...
go test ./apps/facades/facade_open_api/ -run 'Film|DirectorSeries'
go test -race ./...            # full gate
go run ./migrate               # applies 032, 033 and 034 (or psql -f each file)
```
