DROP INDEX IF EXISTS payments_pack_id_idx;

ALTER TABLE payments DROP COLUMN IF EXISTS paid_at;

ALTER TABLE payments DROP COLUMN IF EXISTS pack_id;

DROP TABLE IF EXISTS credit_grants;
