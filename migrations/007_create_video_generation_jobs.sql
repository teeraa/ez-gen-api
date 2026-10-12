-- Migration: 007_create_video_generation_jobs
-- Core job table tracking one generation attempt end-to-end

CREATE TABLE IF NOT EXISTS video_generation_jobs (
    -- Identity
    id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),

    -- Status lifecycle
    status          TEXT NOT NULL DEFAULT 'queued',
    provider_task_id TEXT,
    provider_status  TEXT,

    -- Generation parameters (immutable after creation)
    prompt           TEXT NOT NULL,
    mode             TEXT NOT NULL,
    model_id         TEXT NOT NULL,
    aspect_ratio     TEXT NOT NULL,
    duration_seconds INT  NOT NULL,
    quality          TEXT NOT NULL DEFAULT 'standard',
    enhance_prompt   BOOLEAN NOT NULL DEFAULT FALSE,
    upload_id        UUID REFERENCES uploads(id),

    -- VIOLET DNA links (immutable snapshot references)
    scene_id             UUID REFERENCES scenes(id),
    character_profile_id UUID REFERENCES character_profiles(id),
    style_profile_id     UUID REFERENCES style_profiles(id),

    -- Effective inputs snapshot (immutable; profiles may change after submission)
    effective_inputs JSONB,
    compiled_prompt  TEXT,

    -- Output (set when succeeded)
    output_media_key   TEXT,
    output_thumb_key   TEXT,
    output_media_type  TEXT,
    output_duration_ms INT,

    -- Progress
    progress     INT  NOT NULL DEFAULT 0,
    current_step TEXT,

    -- Failure
    failure_code    TEXT,
    failure_message TEXT,
    provider_error  JSONB,

    -- Credits
    estimated_credits INT,
    charged_credits   INT,

    -- Submission tracking
    submitted_at   TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ,
    last_polled_at TIMESTAMPTZ,

    -- Idempotency (user-scoped; UNIQUE enforced at application layer per user)
    idempotency_key TEXT,

    -- Soft delete
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,

    CONSTRAINT jobs_status_check CHECK (
        status IN ('queued','submitted','processing','succeeded','failed','cancelled')
    ),
    CONSTRAINT jobs_mode_check CHECK (
        mode IN ('text','image_video','image_image')
    ),
    CONSTRAINT jobs_quality_check CHECK (
        quality IN ('standard','high')
    )
);

-- Add FK for scenes.selected_job_id now that the jobs table exists
ALTER TABLE scenes
    ADD CONSTRAINT fk_scenes_selected_job
    FOREIGN KEY (selected_job_id) REFERENCES video_generation_jobs(id);

CREATE INDEX IF NOT EXISTS idx_jobs_user_id         ON video_generation_jobs(user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_status          ON video_generation_jobs(status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_provider_task   ON video_generation_jobs(provider_task_id) WHERE provider_task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_last_polled     ON video_generation_jobs(last_polled_at) WHERE status IN ('submitted','processing');
CREATE INDEX IF NOT EXISTS idx_jobs_user_status     ON video_generation_jobs(user_id, status, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_scene           ON video_generation_jobs(scene_id) WHERE scene_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_idempotency
    ON video_generation_jobs(user_id, idempotency_key) WHERE idempotency_key IS NOT NULL AND deleted_at IS NULL;
