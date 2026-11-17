BEGIN;
-- mode separates the menu a series belongs to: 'director' (ผู้กำกับ) or 'film' (สร้างหนัง).
-- Both menus share episodes, jobs and points; only listing is scoped by mode. Existing rows stay 'director'.
ALTER TABLE director_series
 ADD COLUMN mode text NOT NULL DEFAULT 'director' CHECK (mode IN ('director','film'));
DROP INDEX IF EXISTS director_series_user_updated;
CREATE INDEX director_series_user_mode_updated ON director_series(user_id, mode, updated_at DESC, id DESC) WHERE deleted_at IS NULL;
COMMIT;
