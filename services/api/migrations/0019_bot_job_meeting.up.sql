ALTER TABLE bot_jobs ADD COLUMN meeting_id uuid REFERENCES meetings (id) ON DELETE SET NULL;

CREATE INDEX bot_jobs_meeting_id_idx ON bot_jobs (meeting_id);
