-- Migration: 010_create_scene_characters
-- A scene can feature up to 3 characters. scenes.character_profile_id is kept in
-- sync with the first character (sort_order 0) so existing clients keep working;
-- drop it in a later migration once every client reads scene_characters.

CREATE TABLE IF NOT EXISTS scene_characters (
    scene_id             UUID        NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    character_profile_id UUID        NOT NULL REFERENCES character_profiles(id),
    sort_order           SMALLINT    NOT NULL DEFAULT 0,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scene_id, character_profile_id),
    -- sort_order 0..2 plus uniqueness caps a scene at 3 characters in the database too.
    CONSTRAINT scene_characters_sort_order_check CHECK (sort_order BETWEEN 0 AND 2),
    CONSTRAINT scene_characters_scene_sort_unique UNIQUE (scene_id, sort_order)
);

CREATE INDEX IF NOT EXISTS idx_scene_characters_character ON scene_characters(character_profile_id);

INSERT INTO scene_characters (scene_id, character_profile_id, sort_order)
SELECT id, character_profile_id, 0
FROM scenes
WHERE character_profile_id IS NOT NULL AND deleted_at IS NULL
ON CONFLICT DO NOTHING;
