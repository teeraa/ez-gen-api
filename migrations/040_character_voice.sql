-- Voice bound to a character: the same voices as the Voice menu (preset id or voice_profiles.id)
-- plus emotion/speed/pitch, optionally copied from one of the user's voice_generations ("template").
-- JSON shape: {"voiceId","voiceName","kind":"preset|design|reference","emotion","speed","pitch","templateId"}
BEGIN;
ALTER TABLE library_assets ADD COLUMN IF NOT EXISTS voice jsonb CHECK (voice IS NULL OR jsonb_typeof(voice) = 'object');
ALTER TABLE film_elements  ADD COLUMN IF NOT EXISTS voice jsonb CHECK (voice IS NULL OR jsonb_typeof(voice) = 'object');
COMMIT;
