-- Owned by authz. Saving or removing an organization's own LLM API key and
-- model (QĐ-15) is audited: the key decides whose account pays and where
-- the site's text is sent. Details carry the model and the key's last four
-- characters, never the key.
ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_action_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_action_check
    CHECK (action IN ('token.revoke', 'role.revoke', 'role.grant', 'mfa.enable', 'mfa.disable', 'mfa.recovery_used',
                      'identity.link', 'identity.unlink', 'invitation.create', 'invitation.revoke', 'invitation.accept',
                      'password.reset', 'password.change', 'project.delete', 'project.restore', 'project.purge',
                      'run.delete', 'run.restore', 'run.purge', 'project.configure',
                      'organization.delete', 'organization.restore', 'organization.purge',
                      'organization.ai_settings'));
