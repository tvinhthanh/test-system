-- 0020_invitations — invite people by email (SPEC §11.2 "mời member",
-- §11.6 API6), owned by internal/authz.
--
-- When MAIL is on, granting a role to an email with no account creates an
-- invitation instead: a single-use token (stored as HMAC(pepper, token),
-- like sessions) mailed to that address, valid seven days. Accepting it
-- proves the address, so the account is created verified, gets the role on
-- the node, and signs in. Revoking or accepting ends it.
CREATE TABLE invitations (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    email       text        NOT NULL CHECK (email = lower(email) AND position('@' IN email) > 1 AND length(email) <= 254),
    token_hash  bytea       NOT NULL,
    scope_level text        NOT NULL CHECK (scope_level IN ('organization', 'workspace', 'project')),
    scope_id    uuid        NOT NULL,
    role        text        NOT NULL,
    invited_by  text        NOT NULL,   -- the inviter's email, like authz_audit.actor
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    accepted_at timestamptz,
    revoked_at  timestamptz,
    -- The same role rules as memberships (§11.3.1 "Gán ở tầng").
    CONSTRAINT invitations_role_level_check CHECK (
        (role IN ('org_owner', 'org_admin')           AND scope_level = 'organization')
        OR (role = 'workspace_admin'                   AND scope_level = 'workspace')
        OR (role IN ('maintainer', 'member', 'viewer') AND scope_level = 'project')
    )
);
CREATE INDEX invitations_token ON invitations (token_hash);
-- One open invitation per (email, node): inviting again replaces it.
CREATE UNIQUE INDEX invitations_open ON invitations (email, scope_level, scope_id)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_action_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_action_check
    CHECK (action IN ('token.revoke', 'role.revoke', 'role.grant', 'mfa.enable', 'mfa.disable', 'mfa.recovery_used',
                      'identity.link', 'identity.unlink', 'invitation.create', 'invitation.revoke', 'invitation.accept'));
ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_subject_kind_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_subject_kind_check
    CHECK (subject_kind IN ('api_token', 'membership', 'user', 'invitation'));
