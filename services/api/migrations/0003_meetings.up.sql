CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE folders (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name text NOT NULL,
    parent_id uuid REFERENCES folders (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX folders_workspace_id_idx ON folders (workspace_id);

CREATE INDEX folders_parent_id_idx ON folders (parent_id);

CREATE INDEX folders_workspace_id_created_at_idx ON folders (workspace_id, created_at DESC, id DESC);

CREATE TABLE meetings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    owner_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    title text NOT NULL DEFAULT '',
    source text NOT NULL,
    platform text,
    started_at timestamptz,
    duration_s integer NOT NULL DEFAULT 0,
    consent_state text NOT NULL DEFAULT 'unknown',
    visibility text NOT NULL DEFAULT 'private',
    folder_id uuid REFERENCES folders (id) ON DELETE SET NULL,
    audio_object text,
    transcript_object text,
    status text NOT NULL DEFAULT 'pending',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT meetings_source_valid CHECK (source IN ('desktop', 'bot', 'import')),
    CONSTRAINT meetings_platform_valid CHECK (platform IS NULL OR platform IN ('meet', 'zoom', 'teams')),
    CONSTRAINT meetings_consent_state_valid CHECK (consent_state IN ('unknown', 'pending', 'granted', 'denied')),
    CONSTRAINT meetings_visibility_valid CHECK (visibility IN ('private', 'workspace', 'link')),
    CONSTRAINT meetings_status_valid CHECK (status IN ('pending', 'uploading', 'processing', 'ready', 'failed')),
    CONSTRAINT meetings_duration_non_negative CHECK (duration_s >= 0)
);

CREATE INDEX meetings_workspace_id_idx ON meetings (workspace_id);

CREATE INDEX meetings_owner_user_id_idx ON meetings (owner_user_id);

CREATE INDEX meetings_folder_id_idx ON meetings (folder_id);

CREATE INDEX meetings_workspace_id_created_at_idx ON meetings (workspace_id, created_at DESC, id DESC);

CREATE TABLE transcript_segments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    meeting_id uuid NOT NULL REFERENCES meetings (id) ON DELETE CASCADE,
    seq integer NOT NULL,
    speaker text,
    start_s double precision NOT NULL,
    end_s double precision NOT NULL,
    text text NOT NULL,
    tsv tsvector GENERATED ALWAYS AS (to_tsvector('simple', text)) STORED,
    embedding vector(768),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT transcript_segments_meeting_seq_unique UNIQUE (meeting_id, seq),
    CONSTRAINT transcript_segments_seq_non_negative CHECK (seq >= 0),
    CONSTRAINT transcript_segments_bounds_ordered CHECK (start_s >= 0 AND end_s >= start_s)
);

CREATE INDEX transcript_segments_meeting_id_idx ON transcript_segments (meeting_id);

CREATE INDEX transcript_segments_tsv_idx ON transcript_segments USING gin (tsv);

CREATE INDEX transcript_segments_meeting_id_created_at_idx ON transcript_segments (meeting_id, created_at DESC, id DESC);

CREATE TABLE summaries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    meeting_id uuid NOT NULL REFERENCES meetings (id) ON DELETE CASCADE,
    template_id text,
    language text NOT NULL DEFAULT 'en',
    markdown text NOT NULL DEFAULT '',
    model text NOT NULL DEFAULT '',
    action_items jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX summaries_meeting_id_idx ON summaries (meeting_id);

CREATE INDEX summaries_meeting_id_created_at_idx ON summaries (meeting_id, created_at DESC, id DESC);

CREATE TABLE comments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    meeting_id uuid NOT NULL REFERENCES meetings (id) ON DELETE CASCADE,
    user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    at_s double precision NOT NULL DEFAULT 0,
    body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT comments_at_s_non_negative CHECK (at_s >= 0)
);

CREATE INDEX comments_meeting_id_idx ON comments (meeting_id);

CREATE INDEX comments_user_id_idx ON comments (user_id);

CREATE INDEX comments_meeting_id_created_at_idx ON comments (meeting_id, created_at DESC, id DESC);

CREATE TABLE clips (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    meeting_id uuid NOT NULL REFERENCES meetings (id) ON DELETE CASCADE,
    start_s double precision NOT NULL,
    end_s double precision NOT NULL,
    title text NOT NULL DEFAULT '',
    object text,
    share_token text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT clips_share_token_unique UNIQUE (share_token),
    CONSTRAINT clips_bounds_ordered CHECK (start_s >= 0 AND end_s >= start_s)
);

CREATE INDEX clips_meeting_id_idx ON clips (meeting_id);

CREATE INDEX clips_meeting_id_created_at_idx ON clips (meeting_id, created_at DESC, id DESC);

CREATE TABLE share_links (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    meeting_id uuid NOT NULL REFERENCES meetings (id) ON DELETE CASCADE,
    token text NOT NULL,
    permission text NOT NULL DEFAULT 'view',
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT share_links_token_unique UNIQUE (token),
    CONSTRAINT share_links_permission_valid CHECK (permission IN ('view', 'comment'))
);

CREATE INDEX share_links_meeting_id_idx ON share_links (meeting_id);

CREATE INDEX share_links_meeting_id_created_at_idx ON share_links (meeting_id, created_at DESC, id DESC);
