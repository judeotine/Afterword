CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE meetings
    ADD COLUMN audio_bytes bigint,
    ADD COLUMN transcript_bytes bigint,
    ADD CONSTRAINT meetings_audio_bytes_non_negative CHECK (audio_bytes IS NULL OR audio_bytes >= 0),
    ADD CONSTRAINT meetings_transcript_bytes_non_negative CHECK (transcript_bytes IS NULL OR transcript_bytes >= 0);

CREATE INDEX meetings_workspace_id_folder_id_idx ON meetings (workspace_id, folder_id);

CREATE INDEX meetings_workspace_id_source_idx ON meetings (workspace_id, source);

CREATE INDEX meetings_workspace_id_started_at_idx ON meetings (workspace_id, started_at DESC);

CREATE INDEX meetings_title_trgm_idx ON meetings USING gin (title gin_trgm_ops);

CREATE INDEX transcript_segments_meeting_id_seq_idx ON transcript_segments (meeting_id, seq);
