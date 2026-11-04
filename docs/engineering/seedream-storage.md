# Seedream character generation and private storage

Character and product image generation use `seedream-5-0-260128` by default. Video keeps
`ARK_DEFAULT_MODEL` and the existing ModelArk video registry. No database migration
is needed for this change; the character workflow itself requires migrations
through 015.

## Server configuration

```dotenv
ARK_IMAGE_MODEL=seedream-5-0-260128
ARK_IMAGE_API_KEY=
ARK_IMAGE_BASE_URL=
STORAGE_IMAGE_BUCKET=violet-characters
```

Empty image credentials/endpoint inherit `ARK_API_KEY` and `ARK_BASE_URL`.
Separate image credentials are optional for quota/account separation. Supabase
credentials remain shared for the same project: `SUPABASE_URL` and
`SUPABASE_SERVICE_ROLE_KEY`. These values stay exclusively in the API environment.
No provider or service role secret belongs in Web or a `NEXT_PUBLIC_*` variable.

Create `violet-characters` as a **private** Supabase bucket, with MIME allowlist
`image/png`, `image/jpeg` and file-size limit 25,165,824 bytes (24 MiB).
Keep the existing `STORAGE_BUCKET` unchanged. Bucket creation is an explicit
operator step; startup does not mutate storage configuration. Restart the API
only after configuration and prerequisites are ready. Check pending image jobs
before restarting because the worker resumes queued jobs and generation can cost
credits.

## Durable routing and compatibility

New character jobs persist keys
`characters/<owner UUID>/<job UUID>/character.png` (or `.jpg` for JPEG). A shared
storage adapter routes this namespace to `STORAGE_IMAGE_BUCKET` for uploads,
previews, library reads, downloads, shares and reference signing. Jobs keep their
provider/model snapshot; changing the default does not rewrite queued inputs.
After loading the updated configuration, new jobs capture the new image model;
existing queued jobs continue using their saved model snapshot. An explicit
`ARK_IMAGE_MODEL` overrides the default.

Every existing key, including old generated character images under `outputs/`,
video outputs, uploads, thumbnails, audio and exports, continues to resolve in
`STORAGE_BUCKET`. No objects are copied, moved or deleted. Do not rename either
configured bucket once it contains data without an explicit data migration.
Image bucket failure does not fall back to the video bucket or claim success.
All access URLs remain short-lived signed URLs after application authorization.

The worker validates image decoding and size before uploading and marks success
only after durable storage. Provider rejection/quota/timeout and storage failure
remain failed jobs with actionable errors; no automatic paid generation retry.

## Verification boundary

Reference analysis records a separate `reference_analysis` usage attempt before
calling the provider. Migration `018_reference_analysis_usage.sql` expands the
ledger operation constraint for this attempt; it must be applied through the
normal migration process before using reference analysis. Migration 017 is
unchanged. Reference analysis and image generation remain separate attempts
attributed to the same job, and ledger persistence failure blocks provider calls.
This change does not apply migrations automatically or retry failed jobs.

Tests use fake ModelArk responses and a local HTTP Supabase fixture. They verify
configuration fallback/overrides, new and legacy upload/sign/download routing,
failed image upload isolation and worker output namespace. They do not prove
account activation or live provider output. No paid request is required by tests.

Official provider reference, checked 2026-10-20:
[Seedream API](https://docs.byteplus.com/en/docs/modelark/seedream-4-0-5-0).
The provider lists `seedream-5-0-260128` alongside the supported
`seedream-5-0-lite-260128` alias. Live generation with this configuration has not
been tested.
The model supports `b64_json`; its output can be JPEG, so bucket rules permit both
JPEG and PNG. Model access still depends on the operator's BytePlus account.
