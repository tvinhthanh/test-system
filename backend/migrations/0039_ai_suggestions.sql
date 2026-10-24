-- Owned by aisuggest (QĐ-15).
--
-- An organization's AI choices for drafting test cases: the model, always
-- picked in the dashboard, and whose Anthropic API key pays — its own
-- (sealed: AES-256-GCM, key derived from TOKEN_PEPPER, the organization id
-- as associated data; only the last four characters readable) or, with
-- key_sealed NULL, the server's (LLM_API_KEY). No row: the server's key, if
-- any, with the default model.
CREATE TABLE ai_settings (
    org_id     uuid        PRIMARY KEY,
    model      text        NOT NULL CHECK (model IN ('claude-opus-5-5', 'claude-sonnet-5-5', 'claude-haiku-5-5', 'claude-fable-5-1')),
    key_sealed bytea       CHECK (octet_length(key_sealed) BETWEEN 29 AND 1024),
    key_hint   text        NOT NULL DEFAULT '' CHECK (length(key_hint) <= 8),
    updated_by text        NOT NULL CHECK (length(updated_by) BETWEEN 1 AND 254),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((key_sealed IS NULL) = (key_hint = ''))
);

-- One request for AI-drafted test cases: the test type, the site's address
-- and what the tester wrote go in; drafts come out for the tester to keep or
-- drop — nothing becomes a test case until they pick it. org_id is the
-- project's organization at request time (whose key pays); environment_id is
-- informational. Pruned after 90 days; a project purge removes them.
CREATE TABLE case_suggestions (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid        NOT NULL,
    org_id         uuid        NOT NULL,
    environment_id uuid,
    test_type      text        NOT NULL CHECK (test_type IN (
        'smoke', 'regression', 'e2e',
        'functional', 'performance', 'security', 'usability', 'accessibility', 'compatibility', 'recovery')),
    url            text        CHECK (url IS NULL OR (url ~ '^https?://[^/?#\s]+' AND octet_length(url) <= 2048)),
    context        text        NOT NULL DEFAULT '' CHECK (octet_length(context) <= 4000),
    count          integer     NOT NULL CHECK (count BETWEEN 1 AND 15),
    status         text        NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'done', 'error')),
    drafts         jsonb       CHECK (octet_length(drafts::text) <= 262144),
    page_read      boolean     NOT NULL DEFAULT false,
    model          text        NOT NULL DEFAULT '',
    key_source     text        NOT NULL DEFAULT '' CHECK (key_source IN ('', 'organization', 'server')),
    input_tokens   integer     NOT NULL DEFAULT 0,
    output_tokens  integer     NOT NULL DEFAULT 0,
    error          text        CHECK (octet_length(error) <= 1000),
    requested_by   text        NOT NULL CHECK (length(requested_by) BETWEEN 1 AND 254),
    created_at     timestamptz NOT NULL DEFAULT now(),
    finished_at    timestamptz,
    CHECK ((status = 'done') = (drafts IS NOT NULL)),
    CHECK ((status = 'queued') = (finished_at IS NULL))
);
CREATE INDEX case_suggestions_project ON case_suggestions (project_id, created_at DESC);
CREATE INDEX case_suggestions_age ON case_suggestions (created_at);
