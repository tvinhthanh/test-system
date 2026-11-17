-- 0044_signup_verification — owned by internal/authz. Self-serve sign-up
-- (ADR-001, QĐ-17) proves the address before anything trusts it.
--
-- 0035 created the account on the spot and marked it verified with no mail.
-- A verified address is what Members (add by email), invitations to an
-- existing account and the first OAuth sign-in all treat as its owner, so
-- anyone could sign up as someone else's address first and collect the roles
-- later meant for that person. Now the form only files a pending row and
-- mails a single-use link; the account (verified, with the password chosen
-- there) and its access request are created when the link is opened.
-- Nothing is stored for an address nobody proved but the request itself.
CREATE TABLE signup_requests (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    email          text        NOT NULL CHECK (email = lower(email) AND position('@' IN email) > 1 AND length(email) <= 254),
    scope_level    text        NOT NULL CHECK (scope_level IN ('organization', 'workspace', 'project')),
    scope_id       uuid        NOT NULL,
    requested_role text        NOT NULL,
    note           text        NOT NULL DEFAULT '' CHECK (length(note) <= 1000),
    token_hash     bytea       NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL
);
CREATE UNIQUE INDEX signup_requests_token ON signup_requests (token_hash);
-- Asking again replaces the open request for that address.
CREATE UNIQUE INDEX signup_requests_email ON signup_requests (email);
CREATE INDEX signup_requests_expires ON signup_requests (expires_at);

-- Accounts the first cut created are verified without proof. Un-verify the
-- ones that never proved the mailbox since (a password reset is the proof)
-- and sign them out: sign-in and OAuth refuse an unverified address, and
-- "Forgot password?" (MAIL=smtp) lets the real owner prove it and get in.
WITH unproven AS (
    UPDATE users u SET email_verified_at = NULL
     WHERE u.email_verified_at IS NOT NULL
       AND EXISTS (SELECT 1 FROM access_requests ar WHERE ar.user_id = u.id)
       AND NOT EXISTS (SELECT 1 FROM authz_audit a
                        WHERE a.action = 'password.reset' AND a.subject_kind = 'user' AND a.subject_id = u.id)
    RETURNING u.id
)
UPDATE sessions s SET revoked_at = now()
  FROM unproven
 WHERE s.user_id = unproven.id AND s.revoked_at IS NULL;
