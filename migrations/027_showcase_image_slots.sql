-- Migration: 027_showcase_image_slots
-- Landing-page sections that show a still image: the library character used across the
-- sword-fight shots (flow step 1 + Character Lock) and the generated product image.

ALTER TABLE showcase_items DROP CONSTRAINT IF EXISTS showcase_items_slot_check;
ALTER TABLE showcase_items ADD CONSTRAINT showcase_items_slot_check
    CHECK (slot IN ('reel', 'editor', 'character', 'product'));
