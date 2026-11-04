-- Upload embeds Base, so GORM includes updated_at on inserts and updates.
-- Preserve existing upload records and fill the missing timestamp safely.
-- The migration runner owns the transaction.
ALTER TABLE uploads
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
