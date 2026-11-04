BEGIN;
CREATE TABLE story_flow_projects (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id UUID NOT NULL REFERENCES users(id),
 idempotency_key TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 document JSONB NOT NULL,
 status TEXT NOT NULL CHECK (status IN ('draft','queued','processing','succeeded','failed','needs_attention')),
 lease_token UUID,
 lease_until TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 deleted_at TIMESTAMPTZ,
 UNIQUE(user_id,idempotency_key)
);
CREATE INDEX story_flow_projects_owner ON story_flow_projects(user_id,created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX story_flow_projects_queue ON story_flow_projects(updated_at,id) WHERE deleted_at IS NULL AND status IN ('queued','processing');
COMMIT;
