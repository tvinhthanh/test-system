-- 0025_project_soft_delete — owned by internal/tenancy (SPEC §11.3.2
-- "Tạo/xóa project", §11.7), audit actions owned by internal/authz.
--
-- Deleting a project hides it at once — every route reaches a project
-- through tenancy, which stops returning it — and keeps its data for a
-- window in which it can be restored. After purge_after the worker asks
-- every module to delete what it owns for the project (artifacts in the
-- bucket included), then the row goes. A deleted project's name is free
-- again; restoring into a name taken meanwhile is a conflict.

ALTER TABLE projects ADD COLUMN deleted_at  timestamptz;
ALTER TABLE projects ADD COLUMN deleted_by  text;
ALTER TABLE projects ADD COLUMN purge_after timestamptz;
ALTER TABLE projects ADD CONSTRAINT projects_deleted_shape
    CHECK ((deleted_at IS NULL) = (purge_after IS NULL) AND (deleted_at IS NULL) = (deleted_by IS NULL));

ALTER TABLE projects DROP CONSTRAINT projects_workspace_id_name_key;
CREATE UNIQUE INDEX projects_live_name ON projects (workspace_id, name) WHERE deleted_at IS NULL;
CREATE INDEX projects_purge_due ON projects (purge_after) WHERE deleted_at IS NOT NULL;

ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_action_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_action_check
    CHECK (action IN ('token.revoke', 'role.revoke', 'role.grant', 'mfa.enable', 'mfa.disable', 'mfa.recovery_used',
                      'identity.link', 'identity.unlink', 'invitation.create', 'invitation.revoke', 'invitation.accept',
                      'password.reset', 'password.change', 'project.delete', 'project.restore', 'project.purge'));
ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_subject_kind_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_subject_kind_check
    CHECK (subject_kind IN ('api_token', 'membership', 'user', 'invitation', 'project'));
