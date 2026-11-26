BEGIN;
-- ภาพยนตร์ job queue: indexes that match the claim, the per-user active-job count and the cleanup.
-- film_jobs_claim (status, created_at) could not serve the claim's
-- "queued OR lease ran out" condition; partial indexes keep only the rows each query reads.
DROP INDEX IF EXISTS film_jobs_claim;
CREATE INDEX IF NOT EXISTS film_jobs_queued ON film_jobs (created_at, id) WHERE status = 'queued';
CREATE INDEX IF NOT EXISTS film_jobs_leased ON film_jobs (lease_until) WHERE status IN ('submitting', 'waiting');
-- filmCharge counts a user's active jobs; the claim counts each user's running jobs.
CREATE INDEX IF NOT EXISTS film_jobs_user_active ON film_jobs (user_id, status) WHERE status IN ('queued', 'submitting', 'waiting');
-- Cleanup empties the snapshots of finished jobs and deletes old idempotency receipts.
CREATE INDEX IF NOT EXISTS film_jobs_finished ON film_jobs (updated_at)
 WHERE status IN ('succeeded', 'failed', 'cancelled') AND snapshot <> '{}'::jsonb;
CREATE INDEX IF NOT EXISTS film_receipts_created ON film_receipts (created_at);
COMMIT;
