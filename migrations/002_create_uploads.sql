-- Migration: 002_create_uploads
-- Temporary reference images uploaded before job submission

CREATE TABLE IF NOT EXISTS uploads (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id),
    storage_key TEXT NOT NULL,
    filename    TEXT NOT NULL,
    mime_type   TEXT NOT NULL,
    size_bytes  BIGINT NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_uploads_user_id ON uploads(user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_uploads_expires ON uploads(expires_at) WHERE deleted_at IS NULL;
