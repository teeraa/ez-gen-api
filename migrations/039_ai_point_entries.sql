BEGIN;

-- Point charges for AI features outside Director/Film (video and image jobs, story flow,
-- AI editor, AI chat, voice, marketplace suggestions). The balance is the shared wallet
-- director_point_accounts.
--
-- ai_point_charges holds one row per AI use (feature, ref): charging a ref that is already
-- 'charged' is a no-op, so a retried request is never billed twice; a 'refunded' ref may be
-- charged again when the same request is retried after a failure.
CREATE TABLE ai_point_charges (
  feature text NOT NULL CHECK (char_length(feature) BETWEEN 1 AND 40),
  ref text NOT NULL CHECK (char_length(ref) BETWEEN 1 AND 200),
  user_id uuid NOT NULL REFERENCES users(id),
  status text NOT NULL CHECK (status IN ('pending','charged','refunded')),
  amount integer NOT NULL DEFAULT 0 CHECK (amount >= 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (feature, ref)
);

-- Append-only ledger of every charge and refund.
CREATE TABLE ai_point_entries (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES users(id),
  feature text NOT NULL,
  ref text NOT NULL,
  kind text NOT NULL CHECK (kind IN ('charge','refund')),
  amount integer NOT NULL CHECK (amount >= 0),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ai_point_entries_user_created ON ai_point_entries (user_id, created_at DESC);

COMMIT;
