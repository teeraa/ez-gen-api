-- Migration: 026_showcase_slot
-- Which landing-page section a curated clip belongs to: the scrolling reel, or the
-- AI editor demo timeline (shown in sort_order as consecutive shots).

ALTER TABLE showcase_items ADD COLUMN IF NOT EXISTS slot TEXT NOT NULL DEFAULT 'reel';

DO $$ BEGIN
  ALTER TABLE showcase_items ADD CONSTRAINT showcase_items_slot_check CHECK (slot IN ('reel', 'editor'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DROP INDEX IF EXISTS idx_showcase_items_active_order;
CREATE INDEX IF NOT EXISTS idx_showcase_items_active_slot_order
    ON showcase_items(slot, sort_order, created_at DESC) WHERE is_active;
