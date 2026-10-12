-- Migration: 011_create_video_shares
-- Public share links for finished videos. The token is the only credential for the
-- public page, so it is random (256-bit) and revocable; one active link per job.

CREATE TABLE IF NOT EXISTS video_shares (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id      UUID        NOT NULL REFERENCES video_generation_jobs(id),
    user_id     UUID        NOT NULL REFERENCES users(id),
    token       TEXT        NOT NULL,
    view_count  BIGINT      NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMPTZ,
    CONSTRAINT video_shares_token_unique UNIQUE (token)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_video_shares_active_job
    ON video_shares(job_id) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_video_shares_user ON video_shares(user_id);
