ALTER TABLE meetings
    DROP COLUMN IF EXISTS transcript_etag,
    DROP COLUMN IF EXISTS audio_etag;
