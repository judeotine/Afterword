ALTER TABLE payments DROP CONSTRAINT payments_status_valid;

ALTER TABLE payments ADD CONSTRAINT payments_status_valid
    CHECK (status IN ('pending', 'paid', 'failed', 'refunded', 'needs_review'));

CREATE INDEX payments_needs_review_idx ON payments (created_at DESC) WHERE status = 'needs_review';
