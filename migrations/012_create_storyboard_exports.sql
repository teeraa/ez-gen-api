-- Migration: 012_create_storyboard_exports
-- Async export jobs that merge scene clips into one video with optional audio + text overlays.

CREATE TABLE IF NOT EXISTS storyboard_exports (
    id                 UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID         NOT NULL,
    scene_ids          JSONB        NOT NULL DEFAULT '[]',
    audio_storage_key  TEXT,
    audio_volume       FLOAT        NOT NULL DEFAULT 1.0,
    text_layers        JSONB,
    status             TEXT         NOT NULL DEFAULT 'queued',
    output_storage_key TEXT,
    output_duration_ms BIGINT,
    failure_message    TEXT,
    started_at         TIMESTAMPTZ,
    completed_at       TIMESTAMPTZ,
    last_picked_at     TIMESTAMPTZ,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deleted_at         TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_storyboard_exports_user_id
    ON storyboard_exports(user_id) WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_storyboard_exports_status
    ON storyboard_exports(status) WHERE deleted_at IS NULL;
