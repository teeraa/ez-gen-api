-- Migration: 004_create_character_profiles
-- VIOLET DNA character profiles and their tag + asset associations

CREATE TABLE IF NOT EXISTS character_profiles (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL REFERENCES users(id),
    name              TEXT NOT NULL,
    description       TEXT,
    appearance        TEXT,
    hairstyle         TEXT,
    clothing          TEXT,
    accessories       TEXT,
    defining_features TEXT,
    consistency_notes TEXT,
    version           INT     NOT NULL DEFAULT 1,
    is_active         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at        TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_char_profiles_user ON character_profiles(user_id) WHERE deleted_at IS NULL;

-- Junction: character_profiles ↔ dna_tags
CREATE TABLE IF NOT EXISTS character_profile_tags (
    character_profile_id UUID NOT NULL REFERENCES character_profiles(id) ON DELETE CASCADE,
    tag_id               UUID NOT NULL REFERENCES dna_tags(id),
    PRIMARY KEY (character_profile_id, tag_id)
);

-- Reference images attached to a character profile
CREATE TABLE IF NOT EXISTS character_profile_assets (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    character_profile_id UUID NOT NULL REFERENCES character_profiles(id) ON DELETE CASCADE,
    upload_id            UUID NOT NULL REFERENCES uploads(id),
    role                 TEXT NOT NULL DEFAULT 'reference',
    sort_order           INT  NOT NULL DEFAULT 0,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT char_asset_role_check CHECK (role IN ('reference', 'primary'))
);

CREATE INDEX IF NOT EXISTS idx_char_assets_profile ON character_profile_assets(character_profile_id);
