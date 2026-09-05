DROP TABLE IF EXISTS share_link_requests;

CREATE INDEX transcript_segments_meeting_id_seq_idx ON transcript_segments (meeting_id, seq);

ALTER TABLE share_links ADD COLUMN token text;

UPDATE share_links SET token = token_hash;

ALTER TABLE share_links
    DROP CONSTRAINT share_links_token_hash_unique,
    DROP COLUMN token_hash,
    ALTER COLUMN token SET NOT NULL,
    ADD CONSTRAINT share_links_token_unique UNIQUE (token);

DROP INDEX IF EXISTS meetings_link_sharing_enabled_idx;

ALTER TABLE meetings DROP CONSTRAINT meetings_visibility_valid;

UPDATE meetings SET visibility = 'link' WHERE link_sharing_enabled;

ALTER TABLE meetings ADD CONSTRAINT meetings_visibility_valid CHECK (visibility IN ('private', 'workspace', 'link'));

ALTER TABLE meetings
    DROP CONSTRAINT IF EXISTS meetings_finalize_generation_non_negative,
    DROP COLUMN IF EXISTS finalize_generation,
    DROP COLUMN IF EXISTS link_sharing_enabled;
