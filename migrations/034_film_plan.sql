BEGIN;
-- สร้างหนัง phase 3: chat planning jobs (LLM writes story, elements and shots).
ALTER TABLE film_jobs DROP CONSTRAINT IF EXISTS film_jobs_kind_check;
ALTER TABLE film_jobs ADD CONSTRAINT film_jobs_kind_check CHECK (kind IN ('element_image','shot_video','plan'));
COMMIT;
