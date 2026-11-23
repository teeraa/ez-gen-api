# Character voice binding — plan & API contract (draft)

Status: Phase 1–2 implemented; Phase 3 planned. Goal: a character keeps the **same voice in every shot**, using the
same `seed-audio-1.0` voices as the Voice menu (presets + "My voices" profiles).

## Current state (2026-11)

| Menu | Character entity | Speech today |
|---|---|---|
| Voice menu | – | `seed-audio-1.0`, `voiceId` = preset id or `voice_profiles.id` (`voice_facade.go#resolveVoice`) |
| AI editor | – | `seed-audio-1.0` (`video_editor_voice.go`) |
| Film (สร้างหนัง) | `film_elements` (kind `character`, per series; may be created from a library asset) | Seedance native audio: dialogue written into the video prompt, `GenerateAudio` on; voice differs per shot |
| Director (ผู้กำกับ) | `library_assets` (asset_type `character`) via `characterAssetIds` | none — no dialogue field, `generate_audio` not sent |

Mixing support already exists: `videoprocessing.MergeRequest.AudioTracks` (timeline-positioned tracks over clip audio).

## Decisions (2026-11-20)

1. Approximate lip-sync in Phase 2 is accepted.
2. Voice lines are charged 1 pt per started 10 s of audio.
3. Presets, design voices **and** reference (real recorded) voices can all be bound. A voice can also be
   taken from a past Voice-menu generation as a template (voice + emotion + speed + pitch).

## Phase 1 — bind (implemented)

Migration `040_character_voice.sql`: nullable `voice jsonb` on `library_assets` and `film_elements`
(characters only), shape `models.CharacterVoice`:
`{voiceId, voiceName, kind: "preset"|"design"|"reference", emotion, speed, pitch, templateId?}`.
`voiceName`/`kind` are snapshots, so lists need no lookups.

Request field `voice` (`CharacterVoiceRequest`):

```json
{ "voiceId": "nara", "emotion": "อบอุ่น", "speed": 1.1, "pitch": 0 }   // preset or own profile id
{ "templateId": "<voice_generations.id>" }                              // copy all settings from a generation
{ "voiceId": "" }                                                       // remove
```

Validated by `VoiceFacade.ResolveCharacterVoice` with the Voice-menu rules (emotion list, speed 0.5–2.0,
pitch −12..12); unknown preset, someone else's profile or generation → validation error.

| Method | Path | Change |
|---|---|---|
| PUT | `/api/v1/assets/:id` | optional `voice`; 400 `VALIDATION_ERROR` if the asset is not a character or the voice is invalid |
| POST | `/api/v1/film/series/:id/elements` | optional `voice` (characters). Omitted + `assetId` → copies the library character's voice |
| PATCH | `/api/v1/film/elements/:id` | optional `voice` (with `revision`); 422 `VALIDATION_ERROR` field `voice` |
| GET | asset / element DTOs | `voice: {voiceId, voiceName, kind, emotion, speed, pitch, templateId} \| null` |

Web: `CharacterVoicePicker` (`src/components/voice`) — modes "ไม่ใช้เสียง / เลือกเสียง / จากเมนูเสียง"
(last 20 generations, playable); used in the Library character edit dialog and the Film element editor.
No separate preview endpoint: reference profiles play their sample, templates play their audio.
A deleted profile stays bound but is shown as "เสียงที่ถูกลบแล้ว"; Phase 2 treats it as no voice.

## Phase 2 — film dialogue in the bound voice (implemented, same flow)

No extra step for the user: requesting a shot video (draft, full or finalize) also dubs it.

1. `RequestVideo` freezes `voices[]` into the job (`filmVoiceLine`: speaker, line, delivery, voice) for every
   dialogue line whose speaker has a voice, and adds the voice price to the job cost
   (1 pt per started 10 s, estimated at 12 Thai characters/s; refunded with the job on failure).
2. The video prompt marks those lines as dubbed: the character performs the line (lips move) but the
   provider must not voice it; ambience/effects/music are still generated (`GenerateAudio`).
3. When the provider returns the clip, `dubClip` (film_voice.go) generates each line with
   `VoiceFacade.SpeakLine` (seed-audio, not saved to the Voice-menu history), places lines from 0.4 s with
   0.25 s gaps, lowers the clip audio to 55 % and merges with FFmpeg before the clip is stored — so the shot
   preview, "continue" shots and the export all use the dubbed clip.
4. Transient voice/mix errors retry for up to 30 min from submission; then the shot fails with `VOICE_FAILED`
   (refunded). A line whose voice was deleted is skipped.
5. The shot hash includes the speakers' voices (only for shots with voiced lines), so changing a voice marks
   those clips stale; other shots keep their hash.

Speakers without a voice keep the provider's own speech.

## Phase 3 — director (later)

Director shots have no dialogue yet. Needs a `dialogue[]` field on shots + planner prompt change first; then the same pipeline as Phase 2.

