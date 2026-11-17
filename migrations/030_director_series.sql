BEGIN;
CREATE TABLE director_series (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id),
 name text NOT NULL, aspect_ratio text NOT NULL, style text NOT NULL, resolution text NOT NULL,
 character_asset_ids jsonb NOT NULL DEFAULT '[]'::jsonb, revision integer NOT NULL DEFAULT 1 CHECK(revision >= 1),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz
);
CREATE INDEX director_series_user_updated ON director_series(user_id, updated_at DESC, id DESC) WHERE deleted_at IS NULL;
CREATE TABLE director_series_receipts (
 user_id uuid NOT NULL REFERENCES users(id), key text NOT NULL, hash text NOT NULL,
 series_id uuid NOT NULL REFERENCES director_series(id), created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(user_id,key)
);
ALTER TABLE director_projects
 ADD COLUMN series_id uuid NULL REFERENCES director_series(id),
 ADD COLUMN episode_number integer NULL CHECK(episode_number BETWEEN 1 AND 999),
 ADD COLUMN episode_title text NULL,
 ADD CONSTRAINT director_projects_episode_shape CHECK((series_id IS NULL) = (episode_number IS NULL));
CREATE UNIQUE INDEX director_projects_series_episode ON director_projects(series_id, episode_number) WHERE deleted_at IS NULL AND series_id IS NOT NULL;
CREATE INDEX director_projects_user_standalone ON director_projects(user_id, updated_at DESC, id DESC) WHERE deleted_at IS NULL AND series_id IS NULL;
COMMIT;
