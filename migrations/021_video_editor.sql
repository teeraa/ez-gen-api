BEGIN;
CREATE TABLE video_editor_projects (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), user_id UUID NOT NULL REFERENCES users(id),
 request_id UUID NOT NULL, request_hash TEXT NOT NULL, title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
 aspect_ratio TEXT NOT NULL CHECK(aspect_ratio IN ('9:16','16:9','1:1')), revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 segments JSONB NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(segments)='array' AND jsonb_array_length(segments)<=20),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), deleted_at TIMESTAMPTZ,
 UNIQUE(user_id,request_id), UNIQUE(id,user_id)
);
CREATE INDEX video_editor_projects_owner ON video_editor_projects(user_id,updated_at DESC) WHERE deleted_at IS NULL;
CREATE TABLE video_editor_assets (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), user_id UUID NOT NULL REFERENCES users(id), project_id UUID NOT NULL,
 client_upload_id UUID NOT NULL, content_hash TEXT NOT NULL, name TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('processing','ready','failed')), duration DOUBLE PRECISION NOT NULL DEFAULT 0,
 width INTEGER NOT NULL DEFAULT 0, height INTEGER NOT NULL DEFAULT 0, has_audio BOOLEAN NOT NULL DEFAULT false,
 size_bytes BIGINT NOT NULL CHECK(size_bytes>0 AND size_bytes<=52428800), source_key TEXT NOT NULL DEFAULT '',
 frames JSONB NOT NULL DEFAULT '[]', attempt_token UUID NOT NULL, lease_until TIMESTAMPTZ,
 failure_code TEXT NOT NULL DEFAULT '', failure_message TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), deleted_at TIMESTAMPTZ,
 FOREIGN KEY(project_id,user_id) REFERENCES video_editor_projects(id,user_id), UNIQUE(project_id,user_id,client_upload_id), UNIQUE(id,user_id,project_id)
);
CREATE INDEX video_editor_assets_owner ON video_editor_assets(user_id,project_id,created_at) WHERE deleted_at IS NULL;
CREATE INDEX video_editor_assets_processing ON video_editor_assets(user_id,lease_until) WHERE status='processing' AND deleted_at IS NULL;
CREATE TABLE video_editor_exports (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), user_id UUID NOT NULL REFERENCES users(id), project_id UUID NOT NULL,
 request_id UUID NOT NULL, request_hash TEXT NOT NULL, revision INTEGER NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('queued','processing','completed','failed')), resolution TEXT NOT NULL CHECK(resolution IN ('720p','1080p')),
 aspect_ratio TEXT NOT NULL CHECK(aspect_ratio IN ('9:16','16:9','1:1')), duration DOUBLE PRECISION NOT NULL CHECK(duration>0 AND duration<=1200),
 snapshot JSONB NOT NULL, output_key TEXT NOT NULL DEFAULT '', attempt_token UUID, lease_until TIMESTAMPTZ, attempts INTEGER NOT NULL DEFAULT 0,
 failure_code TEXT NOT NULL DEFAULT '', failure_message TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), deleted_at TIMESTAMPTZ,
 FOREIGN KEY(project_id,user_id) REFERENCES video_editor_projects(id,user_id), UNIQUE(user_id,request_id)
);
CREATE INDEX video_editor_exports_owner ON video_editor_exports(user_id,project_id,created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX video_editor_exports_queue ON video_editor_exports(created_at,id) WHERE status='queued' AND deleted_at IS NULL;
COMMIT;
