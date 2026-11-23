BEGIN;
-- ภาพยนตร์: "shot_redub" jobs mix a character's new voice into a finished clip from its raw clip.
ALTER TABLE film_jobs DROP CONSTRAINT IF EXISTS film_jobs_kind_check;
ALTER TABLE film_jobs ADD CONSTRAINT film_jobs_kind_check CHECK (kind IN ('element_image','shot_video','plan','export','shot_redub'));
COMMIT;
