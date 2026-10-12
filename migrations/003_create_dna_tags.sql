-- Migration: 003_create_dna_tags
-- VIOLET DNA tag library

CREATE TABLE IF NOT EXISTS dna_tags (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug        TEXT NOT NULL,
    name        TEXT NOT NULL,
    category    TEXT NOT NULL,
    description TEXT,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order  INT     NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT dna_tags_slug_unique UNIQUE (slug),
    CONSTRAINT dna_tags_category_check CHECK (
        category IN (
            'character', 'visual_style', 'environment', 'lighting_color',
            'camera', 'motion', 'mood', 'audio', 'quality', 'negative_constraint'
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_dna_tags_category  ON dna_tags(category) WHERE is_active = TRUE;
CREATE INDEX IF NOT EXISTS idx_dna_tags_sort      ON dna_tags(category, sort_order) WHERE is_active = TRUE;

-- Seed data: common tags
INSERT INTO dna_tags (slug, name, category, description, sort_order) VALUES
    -- Visual Style
    ('photorealistic',   'Photorealistic',   'visual_style',   'Ultra-realistic, photo-quality rendering',              10),
    ('anime',            'Anime',            'visual_style',   'Japanese animation art style',                          20),
    ('3d_animation',     '3D Animation',     'visual_style',   'Modern 3D computer-generated animation',                30),
    ('cinematic',        'Cinematic',        'visual_style',   'Film-quality visuals with cinematic framing',           40),
    ('oil_painting',     'Oil Painting',     'visual_style',   'Painterly oil-on-canvas aesthetic',                     50),
    ('watercolor',       'Watercolor',       'visual_style',   'Soft watercolor illustration style',                    60),
    -- Lighting & Color
    ('golden_hour',      'Golden Hour',      'lighting_color', 'Warm sunset/sunrise golden-hour lighting',              10),
    ('blue_hour',        'Blue Hour',        'lighting_color', 'Cool twilight blue-hour lighting',                      20),
    ('neon_purple',      'Neon Purple',      'lighting_color', 'Vibrant purple neon cyberpunk lighting',                30),
    ('moonlight',        'Moonlight',        'lighting_color', 'Cool silvery moonlit scene',                            40),
    ('studio_lit',       'Studio Lit',       'lighting_color', 'Professional studio lighting setup',                    50),
    -- Camera
    ('slow_tracking',    'Slow Tracking',    'camera',         'Slow horizontal or vertical camera track',              10),
    ('drone_aerial',     'Drone Aerial',     'camera',         'Aerial bird''s-eye view camera movement',               20),
    ('dolly_zoom',       'Dolly Zoom',       'camera',         'Classic Hitchcock dolly-zoom effect',                   30),
    ('static_wide',      'Static Wide',      'camera',         'Fixed wide-angle establishing shot',                    40),
    ('handheld',         'Handheld',         'camera',         'Naturalistic handheld camera feel',                     50),
    -- Motion
    ('slow_motion',      'Slow Motion',      'motion',         'High frame-rate slow-motion effect',                    10),
    ('time_lapse',       'Time Lapse',       'motion',         'Accelerated time-lapse motion',                         20),
    ('freeze_frame',     'Freeze Frame',     'motion',         'Brief freeze with surrounding motion',                  30),
    -- Mood
    ('mysterious',       'Mysterious',       'mood',           'Enigmatic, suspenseful atmosphere',                     10),
    ('epic',             'Epic',             'mood',           'Grand, sweeping epic feeling',                          20),
    ('melancholic',      'Melancholic',      'mood',           'Wistful, quietly emotional tone',                       30),
    ('joyful',           'Joyful',           'mood',           'Uplifting, bright, positive mood',                      40),
    -- Environment
    ('urban_night',      'Urban Night',      'environment',    'Nighttime city streets and skyline',                    10),
    ('forest_ancient',   'Ancient Forest',   'environment',    'Deep old-growth woodland',                              20),
    ('desert_vast',      'Vast Desert',      'environment',    'Expansive desert landscape',                            30),
    ('underwater',       'Underwater',       'environment',    'Submerged ocean or lake environment',                   40),
    -- Quality
    ('high_detail',      'High Detail',      'quality',        'Maximum texture and surface detail',                    10),
    ('4k_ultra',         '4K Ultra',         'quality',        'Ultra-high-definition output quality',                  20),
    -- Audio
    ('ambient_score',    'Ambient Score',    'audio',          'Subtle ambient musical background',                     10),
    ('no_music',         'No Music',         'audio',          'Sound effects only, no background music',               20),
    -- Negative Constraints
    ('avoid_text',       'Avoid Text',       'negative_constraint', 'No on-screen text or captions',                    10),
    ('avoid_watermark',  'Avoid Watermark',  'negative_constraint', 'No watermarks or logos',                           20),
    ('avoid_blur',       'Avoid Blur',       'negative_constraint', 'No motion blur or depth-of-field blur',            30)
ON CONFLICT (slug) DO NOTHING;
