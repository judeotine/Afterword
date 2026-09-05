ALTER TABLE meetings
    ADD COLUMN audio_etag text,
    ADD COLUMN transcript_etag text;
