BEGIN;

-- "My voices" for the Voice menu. Two kinds:
--   reference: the user's own recorded/uploaded clip (mono MP3, 5-30 s) sent to seed-audio as @Audio1
--              on every synthesis. consent_at records that the user confirmed the voice is theirs or used with permission.
--   design:    a natural-language voice description written by the user.
CREATE TABLE voice_profiles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id),
  name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 40),
  kind text NOT NULL CHECK (kind IN ('reference','design')),
  description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 300),
  audio_key text,
  audio_seconds double precision NOT NULL DEFAULT 0 CHECK (audio_seconds >= 0),
  audio_bytes bigint NOT NULL DEFAULT 0 CHECK (audio_bytes >= 0),
  consent_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz,
  CHECK ((kind = 'reference' AND audio_key IS NOT NULL AND consent_at IS NOT NULL) OR (kind = 'design' AND char_length(description) >= 5))
);
CREATE INDEX voice_profiles_owner ON voice_profiles(user_id, created_at DESC) WHERE deleted_at IS NULL;

-- History keeps the voice name at generation time, so deleting a profile does not blank old rows.
ALTER TABLE voice_generations ADD COLUMN voice_name text NOT NULL DEFAULT '' CHECK (char_length(voice_name) <= 40);
ALTER TABLE voice_generations ADD COLUMN profile_id uuid REFERENCES voice_profiles(id);

COMMIT;
