-- Owned by autotest (QĐ-16): test cases the server runs by itself.
--
-- A test account of an environment, for steps that need a signed-in user:
-- the password is sealed (AES-256-GCM, key derived from TOKEN_PEPPER for
-- this purpose, the environment id and account key as associated data) and
-- never read back, never sent to a model. login_steps are how to sign in on
-- that site — found once by a model reading the sign-in page with
-- {{username}} / {{password}} in place of the values, or written by a
-- tester — NULL until then. environment_id is tenancy's (no FK, like
-- plans').
CREATE TABLE environment_accounts (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id       uuid        NOT NULL,
    environment_id   uuid        NOT NULL,
    key              text        NOT NULL CHECK (key ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(key) <= 40),
    username         text        NOT NULL CHECK (length(username) BETWEEN 1 AND 254),
    password_sealed  bytea       NOT NULL CHECK (octet_length(password_sealed) BETWEEN 29 AND 2048),
    login_url        text        NOT NULL DEFAULT '' CHECK (octet_length(login_url) <= 2048),
    login_steps      jsonb       CHECK (jsonb_typeof(login_steps) = 'array' AND octet_length(login_steps::text) <= 65536),
    login_status     text        NOT NULL DEFAULT 'unknown' CHECK (login_status IN ('unknown', 'finding', 'ok', 'failed')),
    login_error      text        NOT NULL DEFAULT '' CHECK (octet_length(login_error) <= 1000),
    login_checked_at timestamptz,
    updated_by       text        NOT NULL CHECK (length(updated_by) BETWEEN 1 AND 254),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (environment_id, key)
);
CREATE INDEX environment_accounts_project ON environment_accounts (project_id);

-- What makes a test case runnable: where it starts (an address, relative
-- to the environment's or absolute on the same site), which account signs
-- in ('' = none), the values of its {{variables}} and the steps (see
-- checker/src/steps.mjs for their shape). One per testmgmt case (no FK:
-- another module's table); its human-readable steps are kept in step with
-- these by the api.
CREATE TABLE auto_specs (
    case_id     uuid        PRIMARY KEY,
    project_id  uuid        NOT NULL,
    target      text        NOT NULL CHECK (octet_length(target) BETWEEN 1 AND 2048),
    account     text        NOT NULL DEFAULT '' CHECK (account = '' OR (account ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(account) <= 40)),
    inputs      jsonb       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(inputs) = 'object' AND octet_length(inputs::text) <= 16384),
    steps       jsonb       NOT NULL CHECK (jsonb_typeof(steps) = 'array' AND jsonb_array_length(steps) BETWEEN 1 AND 40
                                            AND octet_length(steps::text) <= 131072),
    source      text        NOT NULL CHECK (source IN ('ai', 'manual')),
    updated_by  text        NOT NULL CHECK (length(updated_by) BETWEEN 1 AND 254),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auto_specs_project ON auto_specs (project_id, updated_at DESC);

-- One request for runnable cases drafted by a model: the worker opens the
-- target (signed in when an account is named), reads the page and asks
-- for cases made of steps. Like case_suggestions (0040): nothing becomes a
-- test case until a tester keeps a draft; org_id is whose key pays.
CREATE TABLE auto_drafts (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid        NOT NULL,
    org_id         uuid        NOT NULL,
    environment_id uuid        NOT NULL,
    target         text        NOT NULL CHECK (octet_length(target) BETWEEN 1 AND 2048),
    account        text        NOT NULL DEFAULT '' CHECK (length(account) <= 40),
    inputs         jsonb       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(inputs) = 'object' AND octet_length(inputs::text) <= 16384),
    context        text        NOT NULL DEFAULT '' CHECK (octet_length(context) <= 4000),
    count          integer     NOT NULL CHECK (count BETWEEN 1 AND 15),
    test_type      text        NOT NULL DEFAULT '' CHECK (test_type IN ('',
        'smoke', 'regression', 'e2e',
        'functional', 'performance', 'security', 'usability', 'accessibility', 'compatibility', 'recovery')),
    status         text        NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'done', 'error')),
    drafts         jsonb       CHECK (octet_length(drafts::text) <= 524288),
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
CREATE INDEX auto_drafts_project ON auto_drafts (project_id, created_at DESC);
CREATE INDEX auto_drafts_age ON auto_drafts (created_at);

-- One run of runnable cases against an environment, asked for in the
-- dashboard or by CI. Each case is one row below and one queue job; the
-- last case to finish closes the run, records it as a test run through the
-- JUnit import (test_run_id) — so the release gate and flaky detection read
-- it like any CI run — next to the testmgmt plan the cases were recorded in
-- (plan_id). heal: a model may repair a step whose element is gone.
CREATE TABLE auto_runs (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid        NOT NULL,
    org_id         uuid        NOT NULL,
    environment_id uuid        NOT NULL,
    plan_id        uuid,
    status         text        NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'error')),
    trigger        text        NOT NULL CHECK (trigger IN ('dashboard', 'ci')),
    requested_by   text        NOT NULL CHECK (length(requested_by) BETWEEN 1 AND 254),
    heal           boolean     NOT NULL DEFAULT true,
    total          integer     NOT NULL DEFAULT 0,
    passed         integer     NOT NULL DEFAULT 0,
    failed         integer     NOT NULL DEFAULT 0,
    errored        integer     NOT NULL DEFAULT 0,
    healed         integer     NOT NULL DEFAULT 0,
    input_tokens   integer     NOT NULL DEFAULT 0,
    output_tokens  integer     NOT NULL DEFAULT 0,
    test_run_id    uuid,
    error          text        CHECK (octet_length(error) <= 1000),
    created_at     timestamptz NOT NULL DEFAULT now(),
    started_at     timestamptz,
    finished_at    timestamptz,
    CHECK ((status IN ('queued', 'running')) = (finished_at IS NULL))
);
CREATE INDEX auto_runs_project ON auto_runs (project_id, created_at DESC, id DESC);
CREATE INDEX auto_runs_active ON auto_runs (project_id) WHERE status IN ('queued', 'running');
CREATE INDEX auto_runs_age ON auto_runs (created_at);

-- One case of a run: the spec as it was when the run started, then what
-- happened at each step (results: [{status, kind, error, ms, healed}]),
-- console errors, a screenshot at the end and one where it failed.
CREATE TABLE auto_run_cases (
    run_id        uuid        NOT NULL REFERENCES auto_runs (id) ON DELETE CASCADE,
    case_id       uuid        NOT NULL,
    position      integer     NOT NULL,
    title         text        NOT NULL CHECK (length(title) BETWEEN 1 AND 500),
    spec          jsonb       NOT NULL CHECK (octet_length(spec::text) <= 196608),
    status        text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'passed', 'failed', 'error')),
    results       jsonb       NOT NULL DEFAULT '[]' CHECK (octet_length(results::text) <= 262144),
    console       jsonb       NOT NULL DEFAULT '[]' CHECK (octet_length(console::text) <= 65536),
    healed        integer     NOT NULL DEFAULT 0,
    duration_ms   integer     NOT NULL DEFAULT 0,
    error         text        CHECK (octet_length(error) <= 1000),
    final_shot    bytea       CHECK (octet_length(final_shot) <= 3145728),
    failure_shot  bytea       CHECK (octet_length(failure_shot) <= 3145728),
    execution_id  uuid,
    finished_at   timestamptz,
    PRIMARY KEY (run_id, case_id),
    CHECK ((status = 'pending') = (finished_at IS NULL))
);
CREATE INDEX auto_run_cases_case ON auto_run_cases (case_id, finished_at DESC);
