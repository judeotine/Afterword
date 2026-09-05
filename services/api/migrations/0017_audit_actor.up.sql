ALTER TABLE audit_log ADD COLUMN actor text NOT NULL DEFAULT '';

CREATE INDEX audit_log_workspace_id_actor_idx ON audit_log (workspace_id, actor);
