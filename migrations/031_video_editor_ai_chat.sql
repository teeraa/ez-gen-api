-- 031: AI chat editor (experimental). One row per chat message in a video editor project.
-- meta holds the assistant's tool trail, timeline proposal and story-job link. Apply after 022.
-- Until it is applied the chat endpoints report unavailable and every other feature keeps working.
BEGIN;
CREATE TABLE IF NOT EXISTS video_editor_ai_chat_messages (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id UUID NOT NULL REFERENCES users(id),
 project_id UUID NOT NULL,
 role TEXT NOT NULL CHECK (role IN ('user','assistant')),
 content TEXT NOT NULL DEFAULT '' CHECK (char_length(content) <= 8000),
 meta JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(meta)='object'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), deleted_at TIMESTAMPTZ,
 FOREIGN KEY (project_id,user_id) REFERENCES video_editor_projects(id,user_id)
);
CREATE INDEX IF NOT EXISTS video_editor_ai_chat_thread ON video_editor_ai_chat_messages(user_id,project_id,created_at DESC,id DESC) WHERE deleted_at IS NULL;
COMMIT;
