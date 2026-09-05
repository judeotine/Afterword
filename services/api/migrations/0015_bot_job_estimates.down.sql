ALTER TABLE bot_jobs DROP CONSTRAINT IF EXISTS bot_jobs_estimated_minutes_positive;

ALTER TABLE bot_jobs DROP COLUMN IF EXISTS bot_name;

ALTER TABLE bot_jobs DROP COLUMN IF EXISTS estimated_minutes;
