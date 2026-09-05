CREATE TABLE keyword_alerts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    phrase text NOT NULL,
    channel text NOT NULL DEFAULT 'email',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT keyword_alerts_channel_valid CHECK (channel IN ('email', 'slack', 'webhook')),
    CONSTRAINT keyword_alerts_phrase_present CHECK (phrase <> '')
);

CREATE INDEX keyword_alerts_workspace_id_idx ON keyword_alerts (workspace_id);

CREATE INDEX keyword_alerts_user_id_idx ON keyword_alerts (user_id);

CREATE INDEX keyword_alerts_workspace_id_created_at_idx ON keyword_alerts (workspace_id, created_at DESC, id DESC);

CREATE TABLE calendar_connections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider text NOT NULL,
    refresh_token_enc bytea NOT NULL,
    auto_join_rule jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT calendar_connections_user_provider_unique UNIQUE (user_id, provider),
    CONSTRAINT calendar_connections_provider_valid CHECK (provider IN ('google', 'microsoft'))
);

CREATE INDEX calendar_connections_user_id_created_at_idx ON calendar_connections (user_id, created_at DESC, id DESC);

CREATE TABLE api_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    hash text NOT NULL,
    scopes text[] NOT NULL DEFAULT '{}'::text[],
    last_used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT api_tokens_hash_unique UNIQUE (hash)
);

CREATE INDEX api_tokens_workspace_id_idx ON api_tokens (workspace_id);

CREATE INDEX api_tokens_workspace_id_created_at_idx ON api_tokens (workspace_id, created_at DESC, id DESC);

CREATE TABLE devices (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name text NOT NULL DEFAULT '',
    platform text NOT NULL,
    last_sync_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT devices_platform_valid CHECK (platform IN ('macos', 'windows', 'linux', 'ios', 'android', 'web'))
);

CREATE INDEX devices_user_id_idx ON devices (user_id);

CREATE INDEX devices_user_id_created_at_idx ON devices (user_id, created_at DESC, id DESC);

CREATE TABLE audit_log (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    actor_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    action text NOT NULL,
    target text NOT NULL DEFAULT '',
    at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT audit_log_action_present CHECK (action <> '')
);

CREATE INDEX audit_log_workspace_id_idx ON audit_log (workspace_id);

CREATE INDEX audit_log_actor_user_id_idx ON audit_log (actor_user_id);

CREATE INDEX audit_log_workspace_id_created_at_idx ON audit_log (workspace_id, created_at DESC, id DESC);
