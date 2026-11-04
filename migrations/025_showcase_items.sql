-- Migration: 025_showcase_items
-- Curated videos for the public landing page. A row is only served while the job
-- still has an active (non-revoked) share link, so the owner's own choice to make
-- the video public is a precondition; revoking the share removes it from the page.

CREATE TABLE IF NOT EXISTS showcase_items (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id      UUID        NOT NULL REFERENCES video_generation_jobs(id),
    title       TEXT        NOT NULL,
    subtitle    TEXT,
    sort_order  INT         NOT NULL DEFAULT 0,
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    created_by  UUID        REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT showcase_items_job_unique UNIQUE (job_id),
    CONSTRAINT showcase_items_title_len CHECK (char_length(title) BETWEEN 1 AND 80),
    CONSTRAINT showcase_items_subtitle_len CHECK (subtitle IS NULL OR char_length(subtitle) <= 60)
);

CREATE INDEX IF NOT EXISTS idx_showcase_items_active_order
    ON showcase_items(sort_order, created_at DESC) WHERE is_active;
