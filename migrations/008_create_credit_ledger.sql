-- Migration: 008_create_credit_ledger
-- Append-only credit accounting ledger

CREATE TABLE IF NOT EXISTS credit_ledger (
    id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    job_id  UUID REFERENCES video_generation_jobs(id),
    amount  INT  NOT NULL,
    reason  TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT ledger_reason_check CHECK (
        reason IN ('job_reservation','job_charge','job_refund','plan_grant')
    )
);

CREATE INDEX IF NOT EXISTS idx_ledger_user ON credit_ledger(user_id);
CREATE INDEX IF NOT EXISTS idx_ledger_job  ON credit_ledger(job_id) WHERE job_id IS NOT NULL;
