BEGIN;

-- สร้างวิดีโอ (studio pipeline) dubbing with a Voice-menu voice, the same way films dub:
-- stage 'voice' generates the line with seed-audio before the video (the video lip-syncs to it),
-- stage 'dub' mixes the clean line over the finished clip. voice holds the stored keys and timing.
ALTER TABLE studio_pipelines ADD COLUMN voice jsonb CHECK (voice IS NULL OR jsonb_typeof(voice) = 'object');
ALTER TABLE studio_pipelines DROP CONSTRAINT studio_pipelines_stage_check;
ALTER TABLE studio_pipelines ADD CONSTRAINT studio_pipelines_stage_check
  CHECK (stage IN ('image','save_character','voice','video','dub','save_video','done'));

COMMIT;
