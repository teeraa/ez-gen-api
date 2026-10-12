-- Migration: 009_add_missing_columns
-- GORM soft-delete requires deleted_at on every model that embeds Base.
-- Asset tables also need updated_at to match the Base struct.

-- dna_tags: add soft-delete + updated_at (currently managed via is_active,
-- but GORM auto-adds WHERE deleted_at IS NULL when the column exists)
ALTER TABLE dna_tags
    ADD COLUMN IF NOT EXISTS updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS deleted_at  TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_dna_tags_deleted ON dna_tags(deleted_at) WHERE deleted_at IS NOT NULL;

-- character_profile_assets: add updated_at + deleted_at
ALTER TABLE character_profile_assets
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- style_profile_assets: add updated_at + deleted_at
ALTER TABLE style_profile_assets
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- scene_assets: add updated_at + deleted_at
ALTER TABLE scene_assets
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- Junction tables: GORM doesn't soft-delete junctions, but add updated_at
-- so the Base struct fields don't cause query issues if ever used directly.
ALTER TABLE character_profile_tags
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE style_profile_tags
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE scene_tags
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
