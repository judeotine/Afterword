ALTER TABLE bot_jobs ADD COLUMN estimated_minutes integer NOT NULL DEFAULT 60;

ALTER TABLE bot_jobs ADD COLUMN bot_name text;

ALTER TABLE bot_jobs ADD CONSTRAINT bot_jobs_estimated_minutes_positive CHECK (estimated_minutes > 0);
