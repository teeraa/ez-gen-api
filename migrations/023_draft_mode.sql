-- 023: add is_draft flag to video_generation_jobs
-- Draft mode is supported only for violet-realism (dreamina-seedance-2-5-260628).
-- The column defaults to false so existing rows are unaffected.

ALTER TABLE video_generation_jobs
  ADD COLUMN IF NOT EXISTS is_draft BOOLEAN NOT NULL DEFAULT FALSE;
