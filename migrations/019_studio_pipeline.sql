BEGIN;
ALTER TABLE library_assets DROP CONSTRAINT library_assets_asset_type_check;
ALTER TABLE library_assets ADD CONSTRAINT library_assets_asset_type_check CHECK (asset_type IN ('character','location','prop','costume','style','video'));
CREATE TABLE studio_pipelines (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id),
 idempotency_key uuid NOT NULL, request_hash text NOT NULL, inputs jsonb NOT NULL,
 status text NOT NULL CHECK (status IN ('queued','processing','succeeded','failed','needs_attention')),
 stage text NOT NULL CHECK (stage IN ('image','save_character','video','save_video','done')),
 character_job_id uuid REFERENCES video_generation_jobs(id), character_asset_id uuid REFERENCES library_assets(id),
 video_job_id uuid REFERENCES video_generation_jobs(id), video_asset_id uuid REFERENCES library_assets(id),
 video_attempt integer NOT NULL DEFAULT 0 CHECK (video_attempt >= 0),
 failure_code text, failure_message text, lease_token uuid, lease_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz,
 UNIQUE(user_id,idempotency_key)
);
CREATE INDEX studio_pipelines_work_idx ON studio_pipelines(status,lease_until,updated_at) WHERE deleted_at IS NULL;
CREATE INDEX studio_pipelines_user_idx ON studio_pipelines(user_id,created_at DESC) WHERE deleted_at IS NULL;
COMMIT;
