BEGIN;

-- Voice menu (/open/v1/voice): text-to-speech made with BytePlus seed-audio-1.0, kept as a per-user history.
-- The audio itself lives in storage under voice/<user>/<id>.mp3; only the key is stored here.
CREATE TABLE voice_generations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id),
  request_id uuid NOT NULL,
  model text NOT NULL,
  text text NOT NULL CHECK (char_length(text) BETWEEN 1 AND 1500),
  voice_id text NOT NULL CHECK (char_length(voice_id) BETWEEN 1 AND 40),
  emotion text NOT NULL DEFAULT '' CHECK (char_length(emotion) <= 40),
  speed numeric(3,1) NOT NULL DEFAULT 1 CHECK (speed BETWEEN 0.5 AND 2),
  pitch integer NOT NULL DEFAULT 0 CHECK (pitch BETWEEN -12 AND 12),
  audio_key text NOT NULL,
  size_bytes bigint NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
  duration double precision NOT NULL DEFAULT 0 CHECK (duration >= 0),
  original_duration double precision NOT NULL DEFAULT 0 CHECK (original_duration >= 0),
  sentences jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(sentences) = 'array'),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
-- A retried POST with the same requestId returns the first result instead of paying for a second synthesis.
CREATE UNIQUE INDEX voice_generations_request ON voice_generations(user_id, request_id);
CREATE INDEX voice_generations_owner ON voice_generations(user_id, created_at DESC) WHERE deleted_at IS NULL;

COMMIT;
