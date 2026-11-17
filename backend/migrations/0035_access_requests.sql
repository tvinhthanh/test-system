-- 0035_access_requests — self-serve sign-up with role approval, variant A
-- "join_org" (ADR-001 / future QĐ-14; reverses §11.2 "no self-signup").
--
-- A visitor signs up and REQUESTS a role on an existing node; they get no
-- membership until an admin of that node approves. The account is created so
-- they can sign in and watch the request, but with zero grants the RBAC
-- union is empty, so they can reach nothing until approved. One open request
-- per (user, node); approving grants the role through the normal path and
-- marks the request approved; denying closes it. Both are audited.
--
-- Email verification on sign-up (needs MAIL=smtp) is a follow-up: in this
-- first cut the human approval is the gate, and the account is created
-- verified so it works without a mailer.
CREATE TABLE access_requests (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    scope_level    text        NOT NULL CHECK (scope_level IN ('organization', 'workspace', 'project')),
    scope_id       uuid        NOT NULL,
    requested_role text        NOT NULL,
    note           text        NOT NULL DEFAULT '' CHECK (length(note) <= 1000),
    status         text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'denied')),
    decided_by     text        NOT NULL DEFAULT '',   -- the approver's email, like authz_audit.actor
    decided_at     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    -- The same role rules as memberships (§11.3.1 "Gán ở tầng").
    CONSTRAINT access_requests_role_level_check CHECK (
        (requested_role IN ('org_owner', 'org_admin')           AND scope_level = 'organization')
        OR (requested_role = 'workspace_admin'                   AND scope_level = 'workspace')
        OR (requested_role IN ('maintainer', 'member', 'viewer') AND scope_level = 'project')
    )
);
-- Admin's queue: the pending requests on a node, newest first.
CREATE INDEX access_requests_pending ON access_requests (scope_level, scope_id)
    WHERE status = 'pending';
-- One open request per (user, node): asking again while one is pending is a
-- conflict, not a second row.
CREATE UNIQUE INDEX access_requests_open ON access_requests (user_id, scope_level, scope_id)
    WHERE status = 'pending';

ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_action_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_action_check
    CHECK (action IN ('token.revoke', 'role.revoke', 'role.grant', 'mfa.enable', 'mfa.disable', 'mfa.recovery_used',
                      'identity.link', 'identity.unlink', 'invitation.create', 'invitation.revoke', 'invitation.accept',
                      'password.reset', 'password.change', 'project.delete', 'project.restore', 'project.purge',
                      'run.delete', 'run.restore', 'run.purge', 'project.configure', 'organization.delete',
                      'organization.restore', 'organization.purge',
                      'access_request.create', 'access_request.approve', 'access_request.deny'));
ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_subject_kind_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_subject_kind_check
    CHECK (subject_kind IN ('api_token', 'membership', 'user', 'invitation', 'project', 'run', 'organization',
                            'access_request'));
