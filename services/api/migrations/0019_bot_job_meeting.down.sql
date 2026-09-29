DROP INDEX IF EXISTS bot_jobs_meeting_id_idx;

ALTER TABLE bot_jobs DROP COLUMN IF EXISTS meeting_id;
