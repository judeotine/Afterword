CREATE TABLE credit_grants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    period date NOT NULL,
    minutes integer NOT NULL,
    expires_at timestamptz NOT NULL,
    expired_at timestamptz,
    expired_minutes integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT credit_grants_workspace_period_unique UNIQUE (workspace_id, period),
    CONSTRAINT credit_grants_minutes_positive CHECK (minutes > 0),
    CONSTRAINT credit_grants_expired_minutes_bounded CHECK (expired_minutes >= 0 AND expired_minutes <= minutes),
    CONSTRAINT credit_grants_expired_at_consistent CHECK (expired_at IS NOT NULL OR expired_minutes = 0)
);

CREATE INDEX credit_grants_workspace_id_idx ON credit_grants (workspace_id);

CREATE INDEX credit_grants_due_idx ON credit_grants (expires_at) WHERE expired_at IS NULL;

CREATE INDEX credit_grants_workspace_id_created_at_id_idx ON credit_grants (workspace_id, created_at DESC, id DESC);

ALTER TABLE payments ADD COLUMN pack_id uuid REFERENCES credit_packs (id) ON DELETE SET NULL;

ALTER TABLE payments ADD COLUMN paid_at timestamptz;

CREATE INDEX payments_pack_id_idx ON payments (pack_id);
