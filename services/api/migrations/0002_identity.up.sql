CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text,
    phone text,
    name text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_identifier_present CHECK (email IS NOT NULL OR phone IS NOT NULL)
);

CREATE UNIQUE INDEX users_email_key ON users (lower(email)) WHERE email IS NOT NULL;

CREATE UNIQUE INDEX users_phone_key ON users (phone) WHERE phone IS NOT NULL;

CREATE INDEX users_created_at_id_idx ON users (created_at DESC, id DESC);

CREATE TABLE workspaces (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    slug text NOT NULL,
    bot_name text NOT NULL DEFAULT 'Afterword Notetaker',
    retention_days integer NOT NULL DEFAULT 365,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT workspaces_slug_unique UNIQUE (slug),
    CONSTRAINT workspaces_slug_format CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
    CONSTRAINT workspaces_retention_days_positive CHECK (retention_days > 0)
);

CREATE INDEX workspaces_created_at_id_idx ON workspaces (created_at DESC, id DESC);

CREATE TABLE memberships (
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id),
    CONSTRAINT memberships_role_valid CHECK (role IN ('owner', 'admin', 'member'))
);

CREATE INDEX memberships_user_id_idx ON memberships (user_id);

CREATE INDEX memberships_workspace_id_created_at_idx ON memberships (workspace_id, created_at DESC, user_id DESC);
