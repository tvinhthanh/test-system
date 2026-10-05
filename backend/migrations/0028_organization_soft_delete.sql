-- 0028_organization_soft_delete — owned by internal/tenancy (SPEC §11.3.2
-- "Billing, xóa org", §11.7), audit actions owned by internal/authz.
--
-- Deleting an organization hides it and everything under it at once —
-- tenancy stops returning the org, its workspaces and its projects, so
-- every route and every CI token below it answers 404 — and keeps the data
-- for a window in which an owner can restore it. After purge_after the
-- worker purges each project the way a deleted project is purged, then the
-- workspaces and the org. The slug stays reserved while the org can come
-- back, so a restore never meets a conflict.

ALTER TABLE organizations ADD COLUMN deleted_at  timestamptz;
ALTER TABLE organizations ADD COLUMN deleted_by  text;
ALTER TABLE organizations ADD COLUMN purge_after timestamptz;
ALTER TABLE organizations ADD CONSTRAINT organizations_deleted_shape
    CHECK ((deleted_at IS NULL) = (purge_after IS NULL) AND (deleted_at IS NULL) = (deleted_by IS NULL));
CREATE INDEX organizations_purge_due ON organizations (purge_after) WHERE deleted_at IS NOT NULL;

ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_action_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_action_check
    CHECK (action IN ('token.revoke', 'role.revoke', 'role.grant', 'mfa.enable', 'mfa.disable', 'mfa.recovery_used',
                      'identity.link', 'identity.unlink', 'invitation.create', 'invitation.revoke', 'invitation.accept',
                      'password.reset', 'password.change', 'project.delete', 'project.restore', 'project.purge',
                      'run.delete', 'run.restore', 'run.purge', 'project.configure',
                      'organization.delete', 'organization.restore', 'organization.purge'));
ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_subject_kind_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_subject_kind_check
    CHECK (subject_kind IN ('api_token', 'membership', 'user', 'invitation', 'project', 'run', 'organization'));
