ALTER TABLE ai_usage DROP CONSTRAINT ai_usage_operation_check;
ALTER TABLE ai_usage ADD CONSTRAINT ai_usage_operation_check
 CHECK (operation IN ('image_generation', 'video_generation', 'reference_analysis'));
