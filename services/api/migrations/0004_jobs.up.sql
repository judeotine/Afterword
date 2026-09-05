CREATE TABLE jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind text NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    idempotency_key text,
    run_at timestamptz NOT NULL DEFAULT now(),
    attempts integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 5,
    locked_by text,
    locked_at timestamptz,
    status text NOT NULL DEFAULT 'pending',
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT jobs_status_valid CHECK (status IN ('pending', 'running', 'succeeded', 'dead')),
    CONSTRAINT jobs_attempts_non_negative CHECK (attempts >= 0),
    CONSTRAINT jobs_max_attempts_positive CHECK (max_attempts > 0),
    CONSTRAINT jobs_kind_present CHECK (kind <> '')
);

CREATE UNIQUE INDEX jobs_kind_idempotency_key_idx ON jobs (kind, idempotency_key) WHERE idempotency_key IS NOT NULL;

CREATE INDEX jobs_status_run_at_idx ON jobs (status, run_at);

CREATE INDEX jobs_status_locked_at_idx ON jobs (status, locked_at);

CREATE INDEX jobs_created_at_id_idx ON jobs (created_at DESC, id DESC);

CREATE TABLE bot_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    meeting_url text NOT NULL,
    platform text NOT NULL,
    scheduled_at timestamptz NOT NULL DEFAULT now(),
    status text NOT NULL DEFAULT 'scheduled',
    worker_id text,
    minutes_used integer NOT NULL DEFAULT 0,
    consent_announced_at timestamptz,
    error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT bot_jobs_platform_valid CHECK (platform IN ('meet', 'zoom', 'teams')),
    CONSTRAINT bot_jobs_status_valid CHECK (status IN ('scheduled', 'claimed', 'joining', 'recording', 'completed', 'failed', 'cancelled')),
    CONSTRAINT bot_jobs_minutes_used_non_negative CHECK (minutes_used >= 0)
);

CREATE INDEX bot_jobs_workspace_id_idx ON bot_jobs (workspace_id);

CREATE INDEX bot_jobs_status_scheduled_at_idx ON bot_jobs (status, scheduled_at);

CREATE INDEX bot_jobs_workspace_id_created_at_idx ON bot_jobs (workspace_id, created_at DESC, id DESC);
