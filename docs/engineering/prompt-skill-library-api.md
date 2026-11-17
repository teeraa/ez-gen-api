# Prompt library & Skills API (CRUD)

Menus: `/studio/prompts` and `/studio/skills` in ez-gen-web. Group: Open API (`/api/v1`, user JWT).
Migration: `033_prompt_library_and_skills.sql` (tables `prompt_templates`, `prompt_bookmarks`, `skills`, plus built-in seed rows).

Built-in rows have `user_id IS NULL`, are visible to every user, and are read-only (`403 BUILT_IN_READ_ONLY`).
Users copy a built-in skill by creating a skill with `forkedFromId`. Another user's rows answer `404`.

## Prompts

| Method | Path | Notes |
|---|---|---|
| GET | `/prompts?scope=all\|mine\|builtin\|saved&category=&q=&page=&limit=` | own first, then built-ins; `limit` ≤ 50 |
| POST | `/prompts` | `{title, category, body}` → 201 |
| GET | `/prompts/:id` | |
| PUT | `/prompts/:id` | full replace, owner only |
| DELETE | `/prompts/:id` | soft delete → 204 |
| PUT / DELETE | `/prompts/:id/bookmark` | idempotent; works on built-ins |

`category`: `video | image | product | character | camera`. Title 2–60 chars, body 5–2000 chars (after trim).

```json
{ "id": "…", "title": "…", "category": "video", "body": "… {ตัวแปร} …", "builtIn": false, "bookmarked": true, "createdAt": "RFC3339", "updatedAt": "RFC3339" }
```

## Skills

| Method | Path | Notes |
|---|---|---|
| GET | `/skills?scope=all\|mine\|builtin&q=&page=&limit=` | |
| POST | `/skills` | `{name, description, steps[], inputs[], output, forkedFromId?}` → 201 |
| GET | `/skills/:id` | |
| PUT | `/skills/:id` | full replace, owner only; `version` +1 |
| DELETE | `/skills/:id` | soft delete → 204 |

- `steps`: 1–8 strings, each ≤ 200 characters. Blank steps are dropped.
- `inputs`: `character | product | style | voice | music | script`. Duplicates are removed.
- `output`: `video_9_16 | video_16_9 | storyboard | image_set`.
- `icon` is copied from the fork source, otherwise `wand`.

```json
{ "id": "…", "name": "…", "description": "…", "icon": "megaphone", "steps": ["…"], "inputs": ["product"], "output": "video_9_16", "builtIn": false, "forkedFromId": "…|null", "version": 2, "createdAt": "…", "updatedAt": "…" }
```

## Errors

These use the standard envelope `{code, message, requestId}`.

| Status | Code | When |
|---|---|---|
| 400 | `VALIDATION_ERROR` | Invalid body or query. |
| 403 | `BUILT_IN_READ_ONLY` | Updating or deleting a built-in. |
| 404 | `NOT_FOUND` | Missing item, another user's item, or a malformed id. |
| 409 | `LIBRARY_LIMIT_REACHED` | The user already has 500 prompts or 200 skills. |

## Not in scope yet

Running a skill ("ใช้สกิลนี้") and sending a prompt into a generator ("ส่งไปหน้าสร้างวิดีโอ") still show the not-connected toast.
Steps are free text for now. Typed steps (`{kind, params}`) come with the skill runner.
