-- 0027_project_retention — owned by internal/artifact (SPEC §10.4, §11.3.2
-- "Sửa retention/cấu hình project"), audit action owned by internal/authz.
--
-- A maintainer may keep a kind of artifact for fewer days than the plan
-- pays for — never more: a row here only ever shortens §10.4. No row means
-- the plan's number. The override is read when an artifact is recorded, so
-- it applies to artifacts from then on; what is stored keeps its expiry.

CREATE TABLE artifact_retention (
    project_id uuid        NOT NULL,
    kind       text        NOT NULL CHECK (kind IN ('screenshot', 'video', 'trace', 'har', 'log', 'attachment', 'manual_evidence')),
    days       integer     NOT NULL CHECK (days BETWEEN 1 AND 3650),
    updated_by text        NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, kind)
);

ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_action_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_action_check
    CHECK (action IN ('token.revoke', 'role.revoke', 'role.grant', 'mfa.enable', 'mfa.disable', 'mfa.recovery_used',
                      'identity.link', 'identity.unlink', 'invitation.create', 'invitation.revoke', 'invitation.accept',
                      'password.reset', 'password.change', 'project.delete', 'project.restore', 'project.purge',
                      'run.delete', 'run.restore', 'run.purge', 'project.configure'));
