-- 0019_identity_unlink — people see and remove the Google / GitHub sign-ins
-- linked to their account (§11.2). Removing one is a security event (§11.7),
-- recorded like linking it.
ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_action_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_action_check
    CHECK (action IN ('token.revoke', 'role.revoke', 'role.grant', 'mfa.enable', 'mfa.disable', 'mfa.recovery_used',
                      'identity.link', 'identity.unlink'));
