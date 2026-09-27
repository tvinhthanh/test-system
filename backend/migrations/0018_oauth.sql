-- 0018_oauth — sign-in with Google or GitHub (SPEC §11.2 "OAuth
-- Google/GitHub"), owned by internal/authz.
--
-- A person is recognised by the provider's stable subject id, never by the
-- email the provider reports today: an email can change hands, a subject
-- does not. The first sign-in links the subject to the existing account with
-- the same verified email; there is still no self-signup (§11.2), so a
-- provider email with no account here is refused.
CREATE TABLE user_identities (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid        NOT NULL REFERENCES users (id),
    provider      text        NOT NULL CHECK (provider IN ('google', 'github')),
    subject       text        NOT NULL CHECK (length(subject) BETWEEN 1 AND 255),
    email         text        NOT NULL,     -- as the provider reported it at link time
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz,
    UNIQUE (provider, subject),
    UNIQUE (user_id, provider)              -- one Google and one GitHub per account
);

-- Linking a provider to an account is a security event (§11.7).
ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_action_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_action_check
    CHECK (action IN ('token.revoke', 'role.revoke', 'role.grant', 'mfa.enable', 'mfa.disable', 'mfa.recovery_used', 'identity.link'));
