DROP INDEX IF EXISTS payments_needs_review_idx;

UPDATE payments SET status = 'failed' WHERE status = 'needs_review';

ALTER TABLE payments DROP CONSTRAINT payments_status_valid;

ALTER TABLE payments ADD CONSTRAINT payments_status_valid
    CHECK (status IN ('pending', 'paid', 'failed', 'refunded'));
