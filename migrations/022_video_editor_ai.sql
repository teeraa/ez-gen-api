BEGIN;
CREATE TABLE video_editor_ai_jobs (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), user_id UUID NOT NULL REFERENCES users(id), project_id UUID NOT NULL,
 request_id UUID NOT NULL, request_hash TEXT NOT NULL, source_revision INTEGER NOT NULL CHECK(source_revision>0),
 phase TEXT NOT NULL CHECK(phase IN ('analysis','planning')),
 status TEXT NOT NULL CHECK(status IN ('queued','processing','awaiting_concept','review','applied','failed','cancelled')),
 stage TEXT NOT NULL CHECK(stage IN ('queued','transcribing','visual_analysis','concepts','planning','review','applied','failed','cancelled')),
 input JSONB NOT NULL, transcripts JSONB NOT NULL DEFAULT '[]', visuals JSONB NOT NULL DEFAULT '[]', concepts JSONB NOT NULL DEFAULT '[]', evidence JSONB NOT NULL DEFAULT '[]', draft JSONB NOT NULL DEFAULT 'null',
 selected_concept_id UUID, completed_assets INTEGER NOT NULL DEFAULT 0,
 analysis_attempts INTEGER NOT NULL DEFAULT 0 CHECK(analysis_attempts BETWEEN 0 AND 3), planning_attempts INTEGER NOT NULL DEFAULT 0 CHECK(planning_attempts BETWEEN 0 AND 3), cloud_calls_used INTEGER NOT NULL DEFAULT 0 CHECK(cloud_calls_used BETWEEN 0 AND 6),
 attempt_token UUID, lease_until TIMESTAMPTZ, retry_at TIMESTAMPTZ, retry_requests JSONB NOT NULL DEFAULT '[]',
 failure_code TEXT NOT NULL DEFAULT '', failure_message TEXT NOT NULL DEFAULT '', applied_request_id UUID, applied_project JSONB NOT NULL DEFAULT 'null',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), deleted_at TIMESTAMPTZ,
 FOREIGN KEY(project_id,user_id) REFERENCES video_editor_projects(id,user_id), UNIQUE(user_id,request_id)
);
CREATE UNIQUE INDEX video_editor_ai_one_active ON video_editor_ai_jobs(user_id) WHERE status IN ('queued','processing') AND deleted_at IS NULL;
CREATE INDEX video_editor_ai_owner ON video_editor_ai_jobs(user_id,project_id,created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX video_editor_ai_queue ON video_editor_ai_jobs(created_at,id) WHERE status='queued' AND deleted_at IS NULL;
CREATE TABLE video_editor_ai_cache (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), user_id UUID NOT NULL REFERENCES users(id), project_id UUID NOT NULL, asset_id UUID NOT NULL,
 content_hash TEXT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('asr','vision')), signature TEXT NOT NULL, result JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), deleted_at TIMESTAMPTZ,
 UNIQUE(user_id,asset_id,content_hash,kind,signature), FOREIGN KEY(asset_id,user_id,project_id) REFERENCES video_editor_assets(id,user_id,project_id)
);
COMMIT;
