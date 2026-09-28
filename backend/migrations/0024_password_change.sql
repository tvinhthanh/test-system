-- 0024_password_change — owned by internal/authz (SPEC §11.2 "đổi password →
-- revoke mọi session khác"): a signed-in person changing their own
-- password is audited like a reset.

ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_action_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_action_check
    CHECK (action IN ('token.revoke', 'role.revoke', 'role.grant', 'mfa.enable', 'mfa.disable', 'mfa.recovery_used',
                      'identity.link', 'identity.unlink', 'invitation.create', 'invitation.revoke', 'invitation.accept',
                      'password.reset', 'password.change'));
