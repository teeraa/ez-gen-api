-- 028: video editor image assets and image layers (stickers, logos).
-- Existing projects get an empty image layer array. Apply after 024; startup never migrates.
-- Until it is applied the API reports imageLayers=false and every other feature keeps working.
BEGIN;
ALTER TABLE video_editor_assets DROP CONSTRAINT IF EXISTS video_editor_assets_kind_check;
ALTER TABLE video_editor_assets
  ADD CONSTRAINT video_editor_assets_kind_check CHECK (kind IN ('video','audio','image'));
ALTER TABLE video_editor_projects
  ADD COLUMN IF NOT EXISTS image_layers JSONB NOT NULL DEFAULT '[]'
  CONSTRAINT video_editor_projects_image_layers_check CHECK (jsonb_typeof(image_layers)='array' AND jsonb_array_length(image_layers)<=20);
COMMIT;
