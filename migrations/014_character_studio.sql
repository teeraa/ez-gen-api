-- Additive phase-one character studio. Apply separately; preserves existing rows.
ALTER TABLE users ADD COLUMN password_hash TEXT;
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin'));
CREATE UNIQUE INDEX users_email_casefold ON users (lower(email));
CREATE TABLE library_assets (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id UUID NOT NULL REFERENCES users(id),
 name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
 asset_type TEXT NOT NULL DEFAULT 'character' CHECK (asset_type IN ('character','location','prop','costume','style')),
 description TEXT NOT NULL DEFAULT '', prompt TEXT NOT NULL DEFAULT '',
 scope TEXT NOT NULL DEFAULT 'personal' CHECK (scope = 'personal'),
 version INT NOT NULL DEFAULT 1,
 job_id UUID REFERENCES video_generation_jobs(id), upload_id UUID REFERENCES uploads(id),
 storage_key TEXT, media_type TEXT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), deleted_at TIMESTAMPTZ,
 CHECK (job_id IS NULL OR upload_id IS NULL)
);
CREATE INDEX library_assets_owner ON library_assets(user_id) WHERE deleted_at IS NULL;
CREATE INDEX library_assets_job ON library_assets(job_id);
CREATE INDEX library_assets_upload ON library_assets(upload_id);
CREATE UNIQUE INDEX library_assets_owner_job ON library_assets(user_id,job_id) WHERE deleted_at IS NULL AND job_id IS NOT NULL;

ALTER TABLE video_generation_jobs DROP CONSTRAINT jobs_mode_check;
ALTER TABLE video_generation_jobs ADD CONSTRAINT jobs_mode_check CHECK (mode IN ('text','image_video','image_image','text_image'));
