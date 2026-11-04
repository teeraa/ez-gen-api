BEGIN;
CREATE TABLE ai_usage (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES users(id),
 job_id uuid NOT NULL REFERENCES video_generation_jobs(id),
 provider text NOT NULL,
 model text NOT NULL,
 operation text NOT NULL CHECK (operation IN ('image_generation','video_generation')),
 provider_task_id text,
 status text NOT NULL CHECK (status IN ('started','submitted','succeeded','failed','cancelled','expired')),
 started_at timestamptz NOT NULL,
 completed_at timestamptz,
 latency_ms bigint CHECK (latency_ms >= 0),
 input_tokens bigint CHECK (input_tokens >= 0),
 output_tokens bigint CHECK (output_tokens >= 0),
 total_tokens bigint CHECK (total_tokens >= 0),
 generated_images bigint CHECK (generated_images >= 0),
 duration_seconds bigint CHECK (duration_seconds >= 0)
);
CREATE INDEX idx_ai_usage_user_started ON ai_usage(user_id, started_at DESC);
CREATE INDEX idx_ai_usage_job ON ai_usage(job_id);
CREATE UNIQUE INDEX idx_ai_usage_provider_task ON ai_usage(provider, provider_task_id) WHERE provider_task_id IS NOT NULL;
COMMIT;
