ALTER TABLE meetings
    ADD COLUMN link_sharing_enabled boolean NOT NULL DEFAULT false,
    ADD COLUMN finalize_generation integer NOT NULL DEFAULT 0,
    ADD CONSTRAINT meetings_finalize_generation_non_negative CHECK (finalize_generation >= 0);

UPDATE meetings SET link_sharing_enabled = true, visibility = 'workspace' WHERE visibility = 'link';

ALTER TABLE meetings DROP CONSTRAINT meetings_visibility_valid;

ALTER TABLE meetings ADD CONSTRAINT meetings_visibility_valid CHECK (visibility IN ('private', 'workspace'));

CREATE INDEX meetings_link_sharing_enabled_idx ON meetings (link_sharing_enabled) WHERE link_sharing_enabled;

ALTER TABLE share_links ADD COLUMN token_hash text;

UPDATE share_links SET token_hash = encode(sha256(token::bytea), 'hex');

ALTER TABLE share_links
    DROP CONSTRAINT share_links_token_unique,
    DROP COLUMN token,
    ALTER COLUMN token_hash SET NOT NULL,
    ADD CONSTRAINT share_links_token_hash_unique UNIQUE (token_hash);

DROP INDEX transcript_segments_meeting_id_seq_idx;

CREATE TABLE share_link_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    request_ip text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX share_link_requests_request_ip_created_at_idx ON share_link_requests (request_ip, created_at DESC);

CREATE INDEX share_link_requests_created_at_idx ON share_link_requests (created_at);
