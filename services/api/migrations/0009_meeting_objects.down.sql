DROP INDEX IF EXISTS transcript_segments_meeting_id_seq_idx;

DROP INDEX IF EXISTS meetings_title_trgm_idx;

DROP INDEX IF EXISTS meetings_workspace_id_started_at_idx;

DROP INDEX IF EXISTS meetings_workspace_id_source_idx;

DROP INDEX IF EXISTS meetings_workspace_id_folder_id_idx;

ALTER TABLE meetings
    DROP CONSTRAINT IF EXISTS meetings_transcript_bytes_non_negative,
    DROP CONSTRAINT IF EXISTS meetings_audio_bytes_non_negative,
    DROP COLUMN IF EXISTS transcript_bytes,
    DROP COLUMN IF EXISTS audio_bytes;
