DROP INDEX IF EXISTS audit_log_workspace_id_actor_idx;

ALTER TABLE audit_log DROP COLUMN IF EXISTS actor;
