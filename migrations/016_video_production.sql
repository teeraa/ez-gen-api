CREATE TABLE IF NOT EXISTS production_projects (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id),
 title text NOT NULL, brief text NOT NULL, aspect_ratio text NOT NULL DEFAULT '16:9', revision integer NOT NULL DEFAULT 1,
 edit_draft jsonb NOT NULL DEFAULT '{"clips":[],"audioVolume":1,"textLayers":[]}',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_production_projects_user ON production_projects(user_id) WHERE deleted_at IS NULL;
ALTER TABLE scenes ADD COLUMN IF NOT EXISTS project_id uuid REFERENCES production_projects(id);
ALTER TABLE scenes ADD COLUMN IF NOT EXISTS reference_asset_id uuid REFERENCES library_assets(id);
ALTER TABLE scenes ADD COLUMN IF NOT EXISTS video_settings jsonb;
CREATE INDEX IF NOT EXISTS idx_scenes_project ON scenes(project_id) WHERE deleted_at IS NULL;
ALTER TABLE storyboard_exports ADD COLUMN IF NOT EXISTS project_id uuid REFERENCES production_projects(id);
ALTER TABLE storyboard_exports ADD COLUMN IF NOT EXISTS idempotency_key uuid;
ALTER TABLE storyboard_exports ADD COLUMN IF NOT EXISTS request_hash text;
ALTER TABLE storyboard_exports ADD COLUMN IF NOT EXISTS output_config jsonb;
CREATE UNIQUE INDEX IF NOT EXISTS idx_exports_idempotency ON storyboard_exports(user_id,idempotency_key) WHERE idempotency_key IS NOT NULL;

ALTER TABLE video_generation_jobs ADD COLUMN IF NOT EXISTS request_hash text;
