-- Migration: 013_add_export_clips
-- Timeline editor: per-clip trim. `clips` supersedes `scene_ids`; scene_ids is kept
-- populated (ordered scene list) for the download/listing paths. Text layer extras
-- (free placement, align, timing) live inside the existing text_layers JSON.

ALTER TABLE storyboard_exports
    ADD COLUMN IF NOT EXISTS clips JSONB;
