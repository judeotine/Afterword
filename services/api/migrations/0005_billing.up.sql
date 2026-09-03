CREATE TABLE credit_locks (
    workspace_id uuid PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE credit_ledger (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    delta_minutes integer NOT NULL,
    reason text NOT NULL,
    ref_id text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT credit_ledger_reason_valid CHECK (reason IN ('grant', 'purchase', 'bot_usage', 'transcribe_usage', 'summary_usage', 'ask_usage', 'refund', 'adjust'))
);

CREATE INDEX credit_ledger_workspace_id_created_at_idx ON credit_ledger (workspace_id, created_at);

CREATE INDEX credit_ledger_workspace_id_created_at_id_idx ON credit_ledger (workspace_id, created_at DESC, id DESC);

CREATE TABLE credit_packs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    minutes integer NOT NULL,
    price_minor bigint NOT NULL,
    currency text NOT NULL,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT credit_packs_minutes_positive CHECK (minutes > 0),
    CONSTRAINT credit_packs_price_non_negative CHECK (price_minor >= 0),
    CONSTRAINT credit_packs_currency_valid CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE INDEX credit_packs_active_idx ON credit_packs (active);

CREATE INDEX credit_packs_created_at_id_idx ON credit_packs (created_at DESC, id DESC);

CREATE TABLE payments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    provider text NOT NULL,
    provider_ref text NOT NULL,
    amount_minor bigint NOT NULL,
    currency text NOT NULL,
    minutes integer NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    raw jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT payments_provider_ref_unique UNIQUE (provider, provider_ref),
    CONSTRAINT payments_status_valid CHECK (status IN ('pending', 'paid', 'failed', 'refunded')),
    CONSTRAINT payments_amount_non_negative CHECK (amount_minor >= 0),
    CONSTRAINT payments_minutes_non_negative CHECK (minutes >= 0),
    CONSTRAINT payments_currency_valid CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE INDEX payments_workspace_id_idx ON payments (workspace_id);

CREATE INDEX payments_workspace_id_created_at_idx ON payments (workspace_id, created_at DESC, id DESC);
