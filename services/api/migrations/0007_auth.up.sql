CREATE TABLE auth_otps (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    channel text NOT NULL,
    destination text NOT NULL,
    code_hash text NOT NULL,
    expires_at timestamptz NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    consumed_at timestamptz,
    request_ip text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT auth_otps_channel_valid CHECK (channel IN ('email', 'phone')),
    CONSTRAINT auth_otps_attempts_non_negative CHECK (attempts >= 0),
    CONSTRAINT auth_otps_destination_present CHECK (length(destination) > 0)
);

CREATE INDEX auth_otps_lookup_idx ON auth_otps (channel, destination, created_at DESC);

CREATE INDEX auth_otps_request_ip_idx ON auth_otps (request_ip, created_at DESC) WHERE request_ip <> '';

CREATE INDEX auth_otps_expires_at_idx ON auth_otps (expires_at);

CREATE TABLE refresh_tokens (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    family_id uuid NOT NULL,
    token_hash text NOT NULL,
    device_id text NOT NULL DEFAULT '',
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    last_used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT refresh_tokens_token_hash_unique UNIQUE (token_hash)
);

CREATE INDEX refresh_tokens_user_id_idx ON refresh_tokens (user_id);

CREATE INDEX refresh_tokens_family_id_idx ON refresh_tokens (family_id);

CREATE INDEX refresh_tokens_expires_at_idx ON refresh_tokens (expires_at);

CREATE INDEX refresh_tokens_user_id_created_at_idx ON refresh_tokens (user_id, created_at DESC, id DESC);

CREATE TABLE oauth_states (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider text NOT NULL,
    state_hash text NOT NULL,
    code_verifier text NOT NULL,
    redirect_to text NOT NULL DEFAULT '',
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT oauth_states_provider_valid CHECK (provider IN ('google')),
    CONSTRAINT oauth_states_state_hash_unique UNIQUE (state_hash)
);

CREATE INDEX oauth_states_expires_at_idx ON oauth_states (expires_at);

CREATE TABLE workspace_invites (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email text NOT NULL,
    role text NOT NULL,
    token_hash text NOT NULL,
    invited_by_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    accepted_by_user_id uuid REFERENCES users (id) ON DELETE SET NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workspace_invites_role_valid CHECK (role IN ('owner', 'admin', 'member')),
    CONSTRAINT workspace_invites_token_hash_unique UNIQUE (token_hash),
    CONSTRAINT workspace_invites_email_format CHECK (email = lower(email) AND position('@' IN email) > 1)
);

CREATE INDEX workspace_invites_workspace_id_idx ON workspace_invites (workspace_id);

CREATE INDEX workspace_invites_email_idx ON workspace_invites (email);

CREATE INDEX workspace_invites_invited_by_user_id_idx ON workspace_invites (invited_by_user_id);

CREATE INDEX workspace_invites_accepted_by_user_id_idx ON workspace_invites (accepted_by_user_id);

CREATE INDEX workspace_invites_workspace_id_created_at_idx ON workspace_invites (workspace_id, created_at DESC, id DESC);

CREATE UNIQUE INDEX workspace_invites_pending_unique ON workspace_invites (workspace_id, email)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;
