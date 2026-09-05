CREATE TABLE auth_otp_verify_attempts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    destination text NOT NULL,
    ip text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT auth_otp_verify_attempts_destination_present CHECK (length(destination) > 0)
);

CREATE INDEX auth_otp_verify_attempts_destination_idx ON auth_otp_verify_attempts (destination, created_at DESC);

CREATE INDEX auth_otp_verify_attempts_ip_idx ON auth_otp_verify_attempts (ip, created_at DESC) WHERE ip <> '';
