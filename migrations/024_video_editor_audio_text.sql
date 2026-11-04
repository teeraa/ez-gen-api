-- 024: video editor audio assets, audio clips and text layers.
-- Existing assets are video; existing projects get empty audio/text arrays.
-- Apply separately after 021 (and 022 when AI is enabled); startup never migrates.
BEGIN;
ALTER TABLE video_editor_assets
  ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'video'
  CONSTRAINT video_editor_assets_kind_check CHECK (kind IN ('video','audio'));
ALTER TABLE video_editor_projects
  ADD COLUMN IF NOT EXISTS audio_clips JSONB NOT NULL DEFAULT '[]'
  CONSTRAINT video_editor_projects_audio_clips_check CHECK (jsonb_typeof(audio_clips)='array' AND jsonb_array_length(audio_clips)<=20),
  ADD COLUMN IF NOT EXISTS text_layers JSONB NOT NULL DEFAULT '[]'
  CONSTRAINT video_editor_projects_text_layers_check CHECK (jsonb_typeof(text_layers)='array' AND jsonb_array_length(text_layers)<=50);
COMMIT;
