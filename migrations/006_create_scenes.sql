-- Migration: 006_create_scenes
-- VIOLET DNA scenes: link character + style profiles; support scene chaining

CREATE TABLE IF NOT EXISTS scenes (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              UUID NOT NULL REFERENCES users(id),
    title                TEXT,
    description          TEXT,
    character_profile_id UUID REFERENCES character_profiles(id),
    style_profile_id     UUID REFERENCES style_profiles(id),
    previous_scene_id    UUID REFERENCES scenes(id),
    scene_order          INT  NOT NULL DEFAULT 0,
    action_override      TEXT,
    camera_override      TEXT,
    setting_override     TEXT,
    selected_job_id      UUID,  -- FK added after video_generation_jobs is created (migration 007)
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at           TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_scenes_user        ON scenes(user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_scenes_char_profile ON scenes(character_profile_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_scenes_style_profile ON scenes(style_profile_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_scenes_previous    ON scenes(previous_scene_id) WHERE previous_scene_id IS NOT NULL;

-- Junction: scenes ↔ dna_tags
CREATE TABLE IF NOT EXISTS scene_tags (
    scene_id UUID NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    tag_id   UUID NOT NULL REFERENCES dna_tags(id),
    PRIMARY KEY (scene_id, tag_id)
);

-- Reference assets for a scene (first/last frame, reference images)
CREATE TABLE IF NOT EXISTS scene_assets (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scene_id   UUID NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    upload_id  UUID NOT NULL REFERENCES uploads(id),
    role       TEXT NOT NULL DEFAULT 'reference',
    sort_order INT  NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT scene_asset_role_check CHECK (
        role IN ('reference', 'first_frame', 'last_frame', 'keyframe')
    )
);

CREATE INDEX IF NOT EXISTS idx_scene_assets_scene ON scene_assets(scene_id);
