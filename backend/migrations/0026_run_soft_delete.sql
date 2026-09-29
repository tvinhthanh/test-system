-- 0026_run_soft_delete — owned by internal/results (SPEC §11.3.2 "Xóa dữ
-- liệu run/artifact", §11.7); audit actions owned by internal/authz.
--
-- Like a project (0025): deleting a run hides it at once — every read of a
-- run or its results carries deleted_at IS NULL, and flaky signals of its
-- tests are rebuilt without it — and keeps it restorable until
-- purge_after, when the worker removes its artifacts (bucket included),
-- triage, review items and results.

ALTER TABLE runs ADD COLUMN deleted_at  timestamptz;
ALTER TABLE runs ADD COLUMN deleted_by  text;
ALTER TABLE runs ADD COLUMN purge_after timestamptz;
ALTER TABLE runs ADD CONSTRAINT runs_deleted_shape
    CHECK ((deleted_at IS NULL) = (purge_after IS NULL) AND (deleted_at IS NULL) = (deleted_by IS NULL));
CREATE INDEX runs_purge_due ON runs (purge_after) WHERE deleted_at IS NOT NULL;

ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_action_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_action_check
    CHECK (action IN ('token.revoke', 'role.revoke', 'role.grant', 'mfa.enable', 'mfa.disable', 'mfa.recovery_used',
                      'identity.link', 'identity.unlink', 'invitation.create', 'invitation.revoke', 'invitation.accept',
                      'password.reset', 'password.change', 'project.delete', 'project.restore', 'project.purge',
                      'run.delete', 'run.restore', 'run.purge'));
ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_subject_kind_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_subject_kind_check
    CHECK (subject_kind IN ('api_token', 'membership', 'user', 'invitation', 'project', 'run'));
