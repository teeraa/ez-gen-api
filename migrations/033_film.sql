BEGIN;
-- สร้างหนัง (film menu). Isolated from Director: its own elements, episode documents, jobs,
-- receipts and point entries. Episodes are the director_projects rows of mode='film' series;
-- the point balance is the shared director_point_accounts wallet.

CREATE TABLE film_elements (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id uuid NOT NULL REFERENCES users(id),
 series_id uuid NOT NULL REFERENCES director_series(id),
 kind text NOT NULL CHECK (kind IN ('character','location','prop')),
 name text NOT NULL,
 spec text NOT NULL DEFAULT '',
 image_key text,
 -- Faceless costume reference used instead of image_key for realistic/cinematic characters.
 synthetic_key text,
 image_status text NOT NULL DEFAULT 'idle' CHECK (image_status IN ('idle','queued','processing','succeeded','failed')),
 failure_code text,
 failure_message text,
 revision integer NOT NULL DEFAULT 1 CHECK (revision >= 1),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 deleted_at timestamptz
);
CREATE INDEX film_elements_series ON film_elements(series_id, kind, created_at) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX film_elements_name ON film_elements(series_id, kind, lower(name)) WHERE deleted_at IS NULL;

CREATE TABLE film_episodes (
 project_id uuid PRIMARY KEY REFERENCES director_projects(id),
 user_id uuid NOT NULL REFERENCES users(id),
 document jsonb NOT NULL DEFAULT '{"shots":[]}'::jsonb,
 revision integer NOT NULL DEFAULT 1 CHECK (revision >= 1),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE film_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id uuid NOT NULL REFERENCES users(id),
 series_id uuid NOT NULL REFERENCES director_series(id),
 project_id uuid REFERENCES director_projects(id),
 kind text NOT NULL CHECK (kind IN ('element_image','shot_video')),
 target_id text NOT NULL,
 status text NOT NULL CHECK (status IN ('queued','submitting','waiting','succeeded','failed','cancelled')),
 snapshot jsonb NOT NULL,
 output bytea,
 cost integer NOT NULL CHECK (cost >= 0),
 refunded boolean NOT NULL DEFAULT false,
 lease_token uuid,
 lease_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX film_jobs_claim ON film_jobs(status, created_at);
CREATE INDEX film_jobs_target ON film_jobs(target_id, status);

CREATE TABLE film_receipts (
 user_id uuid NOT NULL REFERENCES users(id),
 key text NOT NULL,
 hash text NOT NULL,
 target_id text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (user_id, key)
);

CREATE TABLE film_point_entries (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES users(id),
 job_id uuid NOT NULL REFERENCES film_jobs(id),
 kind text NOT NULL CHECK (kind IN ('charge','refund')),
 amount integer NOT NULL CHECK (amount >= 0),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (job_id, kind)
);
COMMIT;
