# Video Data Model Proposal — Agent E Deliverable

> Date: 2026-10-12
> Status: **Proposed** — requires review before any migration is written
> Database: PostgreSQL 15+, accessed via GORM

---

## 1. Design Principles

1. Application-level UUIDs are the primary key for all user-facing entities.
   The BytePlus task ID is an internal field, never exposed to the frontend.
2. Soft-delete on user data (jobs, projects, uploads) to support recovery and audit.
3. All timestamps are UTC, stored as `TIMESTAMPTZ`.
4. Provider errors are stored in a JSONB column — structured, but flexible as the
   provider API evolves. No provider-specific columns in the main schema.
5. Media URLs from BytePlus are ephemeral. The durable media reference is the path
   in the application's object storage bucket.
6. Migrations are written as plain SQL files in `migrations/`. No AutoMigrate in production.

---

## 2. Proposed Tables

### 2.1 `users` (existing or proposed)

> The authentication system has not been finalized. This table is proposed.
> If an external auth provider (e.g., Clerk, Auth0) is used, this table may be minimal.

```sql
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL UNIQUE,
    display_name  TEXT,
    plan          TEXT NOT NULL DEFAULT 'free',   -- 'free' | 'creator' | 'studio_pro'
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMPTZ
);
```

---

### 2.2 `video_generation_jobs`

The central table. Tracks one generation attempt end-to-end.

```sql
CREATE TABLE video_generation_jobs (
    -- Identity
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id),

    -- Lifecycle
    status              TEXT NOT NULL DEFAULT 'queued',
        -- 'queued' | 'submitted' | 'processing' | 'succeeded' | 'failed' | 'cancelled'
    provider_task_id    TEXT,           -- BytePlus task ID (set after submission)
    provider_status     TEXT,           -- Raw provider status value (for debugging)

    -- Generation parameters (immutable after creation)
    prompt              TEXT NOT NULL,
    mode                TEXT NOT NULL,  -- 'text' | 'image_video' | 'image_image'
    model_id            TEXT NOT NULL,  -- Internal model identifier (see model_configs)
    aspect_ratio        TEXT NOT NULL,  -- '16:9' | '9:16' | '1:1'
    duration_seconds    INT NOT NULL,
    quality             TEXT NOT NULL DEFAULT 'standard',  -- 'standard' | 'high'
    enhance_prompt      BOOLEAN NOT NULL DEFAULT FALSE,

    -- Reference image (for image modes)
    upload_id           UUID REFERENCES uploads(id),

    -- Output (set when succeeded)
    output_media_key    TEXT,           -- Object storage key (not a provider URL)
    output_thumb_key    TEXT,           -- Thumbnail object storage key
    output_media_type   TEXT,           -- 'video' | 'image'
    output_duration_ms  INT,            -- Actual output duration in milliseconds

    -- Progress (for polling UX)
    progress            INT NOT NULL DEFAULT 0,  -- 0-100
    current_step        TEXT,           -- Human-readable step label

    -- Failure information
    failure_code        TEXT,           -- Application error code
    failure_message     TEXT,           -- Safe for logging; NOT shown to users as-is
    provider_error      JSONB,          -- Raw provider error payload (redact secrets)

    -- Credits
    estimated_credits   INT,
    charged_credits     INT,

    -- Submission tracking
    submitted_at        TIMESTAMPTZ,    -- When provider task was created
    completed_at        TIMESTAMPTZ,    -- When terminal status was reached
    last_polled_at      TIMESTAMPTZ,    -- For stale job detection

    -- Idempotency
    idempotency_key     TEXT UNIQUE,    -- Client-supplied key to prevent duplicate jobs

    -- Soft delete
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ
);
```

---

### 2.3 `uploads`

Temporary reference images uploaded before job submission.

```sql
CREATE TABLE uploads (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id),
    storage_key     TEXT NOT NULL,      -- Object storage path
    filename        TEXT NOT NULL,
    mime_type       TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,  -- Uploads are temporary
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);
```

---

### 2.4 `credit_ledger`

Append-only ledger for credit accounting. No UPDATE, only INSERTs.

```sql
CREATE TABLE credit_ledger (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    job_id      UUID REFERENCES video_generation_jobs(id),
    amount      INT NOT NULL,       -- Positive = credit, Negative = debit
    reason      TEXT NOT NULL,      -- 'job_reservation' | 'job_charge' | 'job_refund' | 'plan_grant'
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- No soft delete — ledger is immutable
```

---

### 2.5 `model_configs`

Provider model configuration. Allows model-specific parameter validation
without hardcoding in application logic.

```sql
CREATE TABLE model_configs (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id                TEXT NOT NULL UNIQUE,   -- internal: 'violet-motion-2'
    provider_model_id       TEXT NOT NULL,          -- BytePlus: actual API model name
    display_name            TEXT NOT NULL,
    supports_image_video    BOOLEAN NOT NULL DEFAULT FALSE,
    supports_image_image    BOOLEAN NOT NULL DEFAULT FALSE,
    supported_aspect_ratios TEXT[] NOT NULL,
    supported_durations     INT[] NOT NULL,         -- Seconds
    max_prompt_length       INT NOT NULL DEFAULT 2000,
    is_active               BOOLEAN NOT NULL DEFAULT TRUE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

Seed data (to be confirmed against `modelark-integration-research.md`):

```sql
INSERT INTO model_configs (model_id, provider_model_id, display_name, ...) VALUES
    ('violet-motion-2', '<byteplus-model-id>', 'Violet Motion 2.0', ...),
    ('violet-cinema',   '<byteplus-model-id>', 'Violet Cinema',     ...),
    ('violet-fast',     '<byteplus-model-id>', 'Violet Fast',       ...),
    ('violet-realism',  '<byteplus-model-id>', 'Violet Realism',    ...);
-- Actual BytePlus model IDs: see modelark-integration-research.md
```

---

## 3. Indexes

```sql
-- Jobs
CREATE INDEX idx_jobs_user_id          ON video_generation_jobs(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_jobs_status           ON video_generation_jobs(status) WHERE deleted_at IS NULL;
CREATE INDEX idx_jobs_provider_task_id ON video_generation_jobs(provider_task_id) WHERE provider_task_id IS NOT NULL;
CREATE INDEX idx_jobs_last_polled      ON video_generation_jobs(last_polled_at) WHERE status IN ('submitted', 'processing');
CREATE INDEX idx_jobs_user_status      ON video_generation_jobs(user_id, status, created_at DESC) WHERE deleted_at IS NULL;

-- Uploads
CREATE INDEX idx_uploads_user_id  ON uploads(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_uploads_expires  ON uploads(expires_at) WHERE deleted_at IS NULL;

-- Credits
CREATE INDEX idx_ledger_user_id ON credit_ledger(user_id);
CREATE INDEX idx_ledger_job_id  ON credit_ledger(job_id);
```

---

## 4. Job Status State Machine

```
                     ┌─────────┐
         submit ok   │ queued  │  Initial state on POST /jobs
         ──────────► │         │
                     └────┬────┘
                          │ provider.CreateTask() succeeds
                          ▼
                     ┌──────────┐
                     │submitted │  BytePlus task created; waiting for first status
                     └────┬─────┘
                          │ provider status: Processing
                          ▼
                     ┌────────────┐
                     │ processing │  BytePlus is generating
                     └─────┬──────┘
              ┌────────────┼────────────┐
              │ succeeded  │ failed     │ user cancel / provider cancel
              ▼            ▼            ▼
         ┌──────────┐ ┌────────┐ ┌──────────┐
         │succeeded │ │ failed │ │cancelled │
         └──────────┘ └────────┘ └──────────┘
           Terminal     Terminal    Terminal
```

Application status → BytePlus status mapping (confirmed from official SDK):

| Application Status | BytePlus Status | Notes |
|---|---|---|
| `queued` | (not yet submitted) | Job persisted, not yet sent to BytePlus |
| `submitted` | `queued` | BytePlus task created; waiting for BytePlus to start |
| `processing` | `running` | BytePlus is generating |
| `succeeded` | `succeeded` | Output at `response.content.video_url` (~24h lifetime) |
| `failed` | `failed` | `response.error.code` + `response.error.message` |
| `cancelled` | `cancelled` | Only possible if BytePlus status was `queued` at cancel time |

Mapper implementation: `apps/services/modelark/mapper.go`

---

## 5. Concurrent Update Safety

Jobs will be updated by:
1. The HTTP request handler (status → cancelled on DELETE /jobs/:id)
2. The background status poller (status transitions: queued → submitted → processing → succeeded/failed)

Safety rules:
- Status transitions must be conditional: `UPDATE jobs SET status='processing' WHERE status='submitted' AND id=$1`
- The poller must not overwrite a user-initiated cancellation.
- Use PostgreSQL row-level locking (`SELECT FOR UPDATE`) when reading + updating in the same transaction.

---

## 6. Idempotency

The `idempotency_key` column on `video_generation_jobs` allows the frontend to
safely retry a failed POST without creating duplicate jobs.

Behavior:
- If a job with the same `idempotency_key` already exists for the user, return the
  existing job's status instead of creating a new one.
- The key is user-scoped — two users can use the same key independently.
- The key should be a UUID generated by the frontend per-submission.

---

## 7. Media Persistence Strategy

BytePlus output URLs are **ephemeral** (exact lifetime: see `modelark-integration-research.md`).

Required workflow when a job `succeeded`:
1. Retrieve the provider output URL from the BytePlus task response.
2. Download the media to application object storage immediately.
3. Store the object storage key in `output_media_key`.
4. Generate signed URLs from the object storage key on demand (not stored).
5. Never store the provider URL in the database — it will expire.

Object storage options (to be decided):
- BytePlus TOS (Volcengine Object Storage)
- AWS S3
- Cloudflare R2

Decision: Store in `.env` as `STORAGE_PROVIDER`. The `apps/services/storage/`
package should abstract over providers.

---

## 8. Data Lifecycle and Cleanup

| Entity | Retention Rule |
|---|---|
| `video_generation_jobs` (failed/cancelled) | Soft-delete after 90 days |
| `uploads` | Expire after 24 hours; hard-delete via cron |
| `credit_ledger` | Permanent (financial record) |
| Object storage media | Deleted when user deletes project |
| Provider `failure_message` | Sanitize before storing — no secrets |

Cleanup cron jobs are out of scope for the preparation phase but must be planned.

---

## 9. Unresolved Questions

| Question | Blocking | Notes |
|---|---|---|
| Authentication system design | Yes | affects `user_id` in all tables |
| BytePlus output URL lifetime | Yes | drives the urgency of media persistence |
| Object storage provider choice | Yes | affects `output_media_key` format |
| Credit system design | No | `credit_ledger` is prepared; logic TBD |
| Whether `model_configs` is DB or code | No | Could be a Go constant map initially |
| Whether soft-delete is needed on `uploads` | No | Hard-delete on expiry may be simpler |
