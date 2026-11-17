BEGIN;
-- สร้างหนัง phase 4: export jobs merge an episode's clips into one film.
ALTER TABLE film_jobs DROP CONSTRAINT IF EXISTS film_jobs_kind_check;
ALTER TABLE film_jobs ADD CONSTRAINT film_jobs_kind_check CHECK (kind IN ('element_image','shot_video','plan','export'));
COMMIT;
