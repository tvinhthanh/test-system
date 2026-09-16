-- 0009_sessions — login for the dashboard (SPEC §11.2, P1-min column of
-- §4.4: email + password only). Owned by internal/authz.
--
-- Expand step of §8.10: users grows a password hash (argon2id, PHC string)
-- and an email-verified mark; sessions are a table so a login can be
-- revoked server-side ("đổi password → revoke mọi session khác"). The
-- session secret is stored as HMAC-SHA-256(pepper, secret) like API
-- tokens (§11.4): a database dump alone logs nobody in.

ALTER TABLE users
    ADD COLUMN password_hash       text,
    ADD COLUMN password_updated_at timestamptz,
    ADD COLUMN email_verified_at   timestamptz;

CREATE TABLE sessions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id),
    token_hash   bytea       NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz,
    ip           text,
    user_agent   text
);
CREATE INDEX sessions_user ON sessions (user_id) WHERE revoked_at IS NULL;
CREATE INDEX sessions_hash ON sessions (token_hash) WHERE revoked_at IS NULL;

-- §11.2: login rate limit "theo IP và theo account (5 lần/15 phút →
-- backoff)". Counting in a table makes the limit survive a restart and
-- hold across replicas.
CREATE TABLE login_failures (
    key        text        NOT NULL,                              -- "ip:<addr>" or "email:<normalised>"
    failures   integer     NOT NULL DEFAULT 0,
    window_at  timestamptz NOT NULL,                              -- start of the counting window
    locked_to  timestamptz,                                       -- backoff: no attempts accepted before this
    PRIMARY KEY (key)
);
