-- 0002_authz — tables owned by internal/authz (SPEC §6.9.5, §8.8, §11.3).
--
-- P1-min collapses the six roles of §11.3.1 to three (§4.4): admin / member /
-- viewer. Roles and scope levels are text + CHECK rather than Postgres enums
-- so a value can be added or retired with a plain constraint swap (§8.10).
--
-- scope_id is polymorphic (an organization, workspace, project or environment
-- id from tenancy's tables) and therefore has no FOREIGN KEY: the use case
-- that grants a role or issues a token must confirm the scope exists through
-- tenancy first. This is the §6.9.5 boundary, not an oversight.

CREATE TABLE users (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    email      text        NOT NULL UNIQUE
                           CHECK (email = lower(email) AND position('@' IN email) > 1 AND length(email) <= 254),
    created_at timestamptz NOT NULL DEFAULT now()
);
-- password_hash (argon2id, §11.2) arrives with the login use case: expand
-- then, not now.

CREATE TABLE memberships (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users (id),
    scope_level text        NOT NULL CHECK (scope_level IN ('organization', 'workspace', 'project')),
    scope_id    uuid        NOT NULL,
    role        text        NOT NULL CHECK (role IN ('admin', 'member', 'viewer')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, scope_level, scope_id)
);
-- authorize() walks up the tree with one query: memberships of this user on
-- any of (org, workspace, project). Both columns are in the WHERE clause.
CREATE INDEX memberships_user_scope ON memberships (user_id, scope_id);

CREATE TABLE api_tokens (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    prefix       text        NOT NULL CHECK (length(prefix) = 8),
    token_hash   bytea       NOT NULL,   -- HMAC-SHA-256(pepper, secret); §11.4
    scope_level  text        NOT NULL CHECK (scope_level IN ('organization', 'workspace', 'project', 'environment')),
    scope_id     uuid        NOT NULL,
    permissions  text[]      NOT NULL CHECK (cardinality(permissions) >= 1),
    name         text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    created_by   uuid        REFERENCES users (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz
);
-- Lookup is by prefix; the prefix is 32 bits so two tokens may share one and
-- verification compares the hash of every candidate. Not UNIQUE on purpose.
CREATE INDEX api_tokens_prefix ON api_tokens (prefix);
-- The maintainer's "which tokens are dead" view (§11.4).
CREATE INDEX api_tokens_scope ON api_tokens (scope_id) WHERE revoked_at IS NULL;
