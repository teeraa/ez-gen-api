BEGIN;
CREATE TABLE director_projects (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id),
 name text NOT NULL, document jsonb NOT NULL, work_version bigint NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz
);
CREATE INDEX director_projects_user_updated ON director_projects(user_id, updated_at DESC, id DESC) WHERE deleted_at IS NULL;
CREATE TABLE director_jobs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id), project_id uuid NOT NULL REFERENCES director_projects(id),
 kind text NOT NULL, target_id text NOT NULL DEFAULT '', status text NOT NULL,
 snapshot jsonb NOT NULL, output bytea, cost integer NOT NULL CHECK(cost >= 0), refunded boolean NOT NULL DEFAULT false,
 lease_token uuid, lease_until timestamptz, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz
);
CREATE INDEX director_jobs_claim ON director_jobs(status, created_at) WHERE deleted_at IS NULL;
CREATE INDEX director_jobs_project ON director_jobs(project_id, status);
CREATE TABLE director_receipts (
 user_id uuid NOT NULL REFERENCES users(id), key text NOT NULL, hash text NOT NULL,
 project_id uuid NOT NULL REFERENCES director_projects(id), response jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(user_id,key)
);
CREATE TABLE director_point_accounts (user_id uuid PRIMARY KEY REFERENCES users(id), balance bigint NOT NULL DEFAULT 0 CHECK(balance >= 0));
CREATE TABLE director_point_entries (id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id), job_id uuid NOT NULL REFERENCES director_jobs(id), kind text NOT NULL CHECK(kind IN ('charge','refund')), amount integer NOT NULL CHECK(amount >= 0), created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(job_id,kind));
COMMIT;
