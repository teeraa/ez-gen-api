# Voice menu API (`/api/v1/voice`)

Text-to-speech for the web Voice menu (`/studio/voice`, tab "พากย์เสียง") using BytePlus
`seed-audio-1.0` (`apps/services/seedaudio`). Results are saved per user (migration 037,
table `voice_generations`) and the MP3 is stored at `voice/<userId>/<id>.mp3`.
The music and sound-effect tabs are not connected.

Voices are natural-language descriptions defined in `voice_facade.go` (`voicePresets`);
no BytePlus speaker ID is used. Emotion adds a mood phrase to the description.
Text is capped at 1500 characters so Thai narration stays under the model's 120-second output cap.

| Method | Path | Notes |
|---|---|---|
| GET | `/voice/capabilities` | `{available, model, maxChars, voices[{id,name,description,color}], emotions[]}`; `available` is false without `BYTEPLUS_VOICE_API_KEY` |
| GET | `/voice/generations?page&limit` | newest first, `limit` ≤ 50 |
| POST | `/voice/generations` | body `{requestId(uuid), text, voiceId, emotion, speed(0.5–2.0), pitch(-12–12)}`; 201 new, 200 replay of the same `requestId` (no second provider call), 409 `IDEMPOTENCY_CONFLICT` when the same `requestId` has a different body |
| DELETE | `/voice/generations/:id` | soft delete; the stored file is kept |

Item: `{id, text, voiceId, voiceName, emotion, speed, pitch, duration, sizeBytes, audioUrl, downloadUrl, expiresAt, sentences[{start,end,text}], createdAt}`.
URLs are signed for one hour; list again to refresh.

`speed` maps to `speech_rate = round((speed-1)*100)` (0.5× → -50, 2.0× → 100).

Errors: 422 `VALIDATION_ERROR` / `VOICE_REJECTED`, 429 `RATE_LIMITED` (10/min/user) / `VOICE_BUSY` (2 in flight per user),
502 `VOICE_FAILED`, 503 `VOICE_UNAVAILABLE` / `VOICE_NOT_GRANTED` (BytePlus 401/403) / `STORAGE_UNAVAILABLE`.
Generation is not charged in points yet.

## My voices (migration 038, table `voice_profiles`)

A generation's `voiceId` may also be the id of one of the caller's profiles; history keeps
`voiceName` and `profileId` so deleting a profile does not blank old rows. Max 20 profiles per user.

| Method | Path | Notes |
|---|---|---|
| GET | `/voice/profiles` | `{items:[{id,name,kind,description,audioSeconds,sampleUrl,expiresAt,createdAt}]}` |
| POST | `/voice/profiles/design` | JSON `{name(1–40), description(5–300)}`; the description becomes the voice prompt |
| POST | `/voice/profiles/reference` | multipart `name`, `consent=true`, `file` (upload or browser recording, ≤30 MB). Converted with FFmpeg to mono 24 kHz MP3, trimmed to 29.5 s, must be ≥5 s; stored at `voice-profiles/<user>/<id>.mp3` |
| DELETE | `/voice/profiles/:id` | soft delete |

Reference voices are sent to seed-audio as `references[0].audio_data` with the prompt `@Audio1 reads…`,
and with `watermark.aigc_metadata` enabled (producer "VIOLET STUDIO"). No BytePlus Voice Replication is used.

Errors: 409 `VOICE_PROFILE_LIMIT`, 422 `VOICE_CONSENT_REQUIRED` / `VOICE_SAMPLE_TOO_SHORT` / `INVALID_FILE_TYPE`,
503 `MEDIA_UNAVAILABLE` (no FFmpeg). `capabilities` adds `maxProfiles`, `referenceAvailable`, `referenceMinSeconds`, `referenceMaxSeconds`, `referenceMaxBytes`.
