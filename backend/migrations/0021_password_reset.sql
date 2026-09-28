-- 0021_password_reset — "forgot password" (SPEC §11.2), owned by
-- internal/authz. A reset is a single-use token mailed to the account's
-- address, stored as HMAC(pepper, token), valid thirty minutes; asking again
-- replaces the open one. Using it sets the password and revokes every
-- session (§11.2 "đổi password → revoke mọi session khác").
CREATE TABLE password_resets (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id),
    token_hash bytea       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    revoked_at timestamptz
);
CREATE INDEX password_resets_token ON password_resets (token_hash);
CREATE UNIQUE INDEX password_resets_open ON password_resets (user_id) WHERE used_at IS NULL AND revoked_at IS NULL;

ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_action_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_action_check
    CHECK (action IN ('token.revoke', 'role.revoke', 'role.grant', 'mfa.enable', 'mfa.disable', 'mfa.recovery_used',
                      'identity.link', 'identity.unlink', 'invitation.create', 'invitation.revoke', 'invitation.accept',
                      'password.reset'));
