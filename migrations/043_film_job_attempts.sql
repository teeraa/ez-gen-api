BEGIN;
-- ภาพยนตร์: a job that keeps crashing the worker is stopped and refunded instead of being claimed
-- forever. attempts counts consecutive claims of a lease that ran out (or was released after a
-- panic) without the worker finishing or parking the job; it goes back to 0 when the job parks
-- itself to wait. last_error keeps the latest crash for support.
ALTER TABLE film_jobs ADD COLUMN IF NOT EXISTS attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0);
ALTER TABLE film_jobs ADD COLUMN IF NOT EXISTS last_error text CHECK (last_error IS NULL OR char_length(last_error) <= 4000);
COMMIT;
