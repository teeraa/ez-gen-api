-- Migration: 005_create_style_profiles
-- VIOLET DNA style profiles and their tag + asset associations

CREATE TABLE IF NOT EXISTS style_profiles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id),
    name            TEXT NOT NULL,
    description     TEXT,
    visual_style    TEXT,
    color_palette   TEXT,
    lighting        TEXT,
    mood            TEXT,
    camera_language TEXT,
    quality_notes   TEXT,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_style_profiles_user ON style_profiles(user_id) WHERE deleted_at IS NULL;

-- Junction: style_profiles ↔ dna_tags
CREATE TABLE IF NOT EXISTS style_profile_tags (
    style_profile_id UUID NOT NULL REFERENCES style_profiles(id) ON DELETE CASCADE,
    tag_id           UUID NOT NULL REFERENCES dna_tags(id),
    PRIMARY KEY (style_profile_id, tag_id)
);

-- Reference assets attached to a style profile
CREATE TABLE IF NOT EXISTS style_profile_assets (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    style_profile_id UUID NOT NULL REFERENCES style_profiles(id) ON DELETE CASCADE,
    upload_id        UUID NOT NULL REFERENCES uploads(id),
    role             TEXT NOT NULL DEFAULT 'reference',
    sort_order       INT  NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT style_asset_role_check CHECK (role IN ('reference'))
);

CREATE INDEX IF NOT EXISTS idx_style_assets_profile ON style_profile_assets(style_profile_id);
