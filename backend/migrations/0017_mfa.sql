-- 0017_mfa — TOTP second factor (SPEC §11.2 "MFA TOTP cho org owner"),
-- owned by internal/authz.
--
-- The TOTP secret must be readable by the server (it computes the code), so
-- unlike passwords it cannot be hashed: it is sealed with AES-GCM under a key
-- derived from TOKEN_PEPPER, so a database dump alone yields no second
-- factor. totp_pending_secret holds a secret between "set up" and the first
-- valid code; totp_last_step makes a code single-use (RFC 6238 §5.2).
ALTER TABLE users
    ADD COLUMN totp_secret         bytea,
    ADD COLUMN totp_pending_secret bytea,
    ADD COLUMN totp_enabled_at     timestamptz,
    ADD COLUMN totp_last_step      bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT users_totp_enabled CHECK ((totp_enabled_at IS NULL) = (totp_secret IS NULL));

-- Ten one-time codes per enrolment, stored as HMAC(pepper, code) like API
-- tokens; re-enrolling replaces them.
CREATE TABLE mfa_recovery_codes (
    id        uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   uuid        NOT NULL REFERENCES users (id),
    code_hash bytea       NOT NULL,
    used_at   timestamptz
);
CREATE INDEX mfa_recovery_codes_user ON mfa_recovery_codes (user_id) WHERE used_at IS NULL;

-- Between a correct password and a correct code: a short-lived challenge,
-- stored as an HMAC like sessions. No session exists until the code is right.
CREATE TABLE mfa_challenges (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id),
    token_hash bytea       NOT NULL,
    expires_at timestamptz NOT NULL,
    ip         text,
    user_agent text
);
CREATE INDEX mfa_challenges_hash ON mfa_challenges (token_hash);

-- Turning a second factor on or off, and spending a recovery code, are
-- security events (§11.7): recorded like revocations.
ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_action_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_action_check
    CHECK (action IN ('token.revoke', 'role.revoke', 'role.grant', 'mfa.enable', 'mfa.disable', 'mfa.recovery_used'));
ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_subject_kind_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_subject_kind_check
    CHECK (subject_kind IN ('api_token', 'membership', 'user'));
