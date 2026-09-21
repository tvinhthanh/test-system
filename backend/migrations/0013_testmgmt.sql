-- 0013_testmgmt — tables owned by internal/testmgmt (SPEC §6.8, §6.9.5).
--
-- The Test Management Plane (P2, QĐ-12). Its one reason to exist is the
-- third link of §6.8.1: a manual test case tied to a TestIdentity, so that
-- automation status and flaky history flow up to requirement and release.
-- Every link points at test_identities.id — never at a run, a result or a
-- clientTestId (§6.8.1). No FK across modules; the identity is checked
-- through identity's contract at write time.
--
-- Manual execution is not a Run (§6.8.6): it lives here, never in results.

CREATE TABLE test_suites (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  uuid        NOT NULL,
    parent_id   uuid        REFERENCES test_suites (id) ON DELETE SET NULL,
    name        text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, parent_id, name)
);

CREATE TABLE requirements (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   uuid        NOT NULL,
    external_key text        CHECK (external_key IS NULL OR length(external_key) BETWEEN 1 AND 100),   -- JIRA-123, Linear id
    title        text        NOT NULL CHECK (length(title) BETWEEN 1 AND 500),
    priority     text        NOT NULL DEFAULT 'p2' CHECK (priority IN ('p0', 'p1', 'p2', 'p3')),         -- §6.8.4 gate reads p0 (N62)
    status       text        NOT NULL DEFAULT 'active' CHECK (status IN ('draft', 'active', 'done', 'dropped')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX requirements_project_key ON requirements (project_id, external_key) WHERE external_key IS NOT NULL;
CREATE INDEX requirements_project ON requirements (project_id, status, priority, created_at);

CREATE TABLE test_cases (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        uuid        NOT NULL,
    suite_id          uuid        REFERENCES test_suites (id) ON DELETE SET NULL,
    external_key      text        CHECK (external_key IS NULL OR length(external_key) BETWEEN 1 AND 100),   -- import upsert key (TC-42, TestRail C123)
    title             text        NOT NULL CHECK (length(title) BETWEEN 1 AND 500),
    steps             jsonb       NOT NULL DEFAULT '[]'::jsonb,   -- [{action, expected}] — rendered as text, never HTML (QĐ-9 #3)
    expected          text        NOT NULL DEFAULT '',
    priority          text        NOT NULL DEFAULT 'p2' CHECK (priority IN ('p0', 'p1', 'p2', 'p3')),
    is_critical       boolean     NOT NULL DEFAULT false,         -- release gate §6.8.4
    automation_status text        NOT NULL DEFAULT 'manual_only' CHECK (automation_status IN ('manual_only', 'automation_candidate', 'automated', 'obsolete')),
    owner_id          uuid,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX test_cases_project_key ON test_cases (project_id, external_key) WHERE external_key IS NOT NULL;
CREATE INDEX test_cases_project ON test_cases (project_id, automation_status, priority, created_at);
CREATE INDEX test_cases_suite ON test_cases (suite_id);

-- The link that makes the plane worth building (§6.8.1). project_id is
-- denormalized so tenant scoping stays inside one WHERE (§11.3.3), as N04
-- did for aliases.
CREATE TABLE test_case_automation_links (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid        NOT NULL,
    test_case_id   uuid        NOT NULL REFERENCES test_cases (id) ON DELETE CASCADE,
    stable_test_id uuid        NOT NULL,                          -- test_identities.id; identity's table, no FK
    link_type      text        NOT NULL DEFAULT 'exact' CHECK (link_type IN ('exact', 'partial', 'superset')),
    created_by     text        NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (test_case_id, stable_test_id)
);
CREATE INDEX test_case_links_identity ON test_case_automation_links (project_id, stable_test_id);

CREATE TABLE requirement_coverage (
    project_id     uuid        NOT NULL,
    requirement_id uuid        NOT NULL REFERENCES requirements (id) ON DELETE CASCADE,
    test_case_id   uuid        NOT NULL REFERENCES test_cases (id) ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (requirement_id, test_case_id)
);
CREATE INDEX requirement_coverage_case ON requirement_coverage (test_case_id);

CREATE TABLE test_plans (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid        NOT NULL,
    name           text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    environment_id uuid,                                          -- tenancy's table, informational
    status         text        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'closed')),
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX test_plans_project ON test_plans (project_id, status, created_at);

-- One manual run of one case inside a plan. Append-only: a re-test is a new
-- row, the latest row per (plan, case) is the plan's current verdict.
CREATE TABLE manual_executions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   uuid        NOT NULL,
    test_plan_id uuid        NOT NULL REFERENCES test_plans (id) ON DELETE CASCADE,
    test_case_id uuid        NOT NULL REFERENCES test_cases (id) ON DELETE CASCADE,
    executed_by  text        NOT NULL,
    status       text        NOT NULL CHECK (status IN ('passed', 'failed', 'blocked', 'skipped')),
    note         text        NOT NULL DEFAULT '',
    executed_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX manual_executions_plan ON manual_executions (test_plan_id, test_case_id, executed_at DESC);

-- Release = the thing the gate speaks about (§6.8.4). last_gate is the most
-- recent evaluation, machine-readable, so CI polls a stored answer and the
-- p0-coverage rule can compare against the previous release's figure.
CREATE TABLE releases (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid        NOT NULL,
    version        text        NOT NULL CHECK (length(version) BETWEEN 1 AND 100),
    branch         text        NOT NULL DEFAULT '',                -- '' = any branch: the gate reads the newest finished run
    status         text        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'blocked', 'approved', 'shipped')),
    decided_by     text,                                          -- §6.8.4 #1: an override carries a name
    decided_at     timestamptz,
    decision_note  text        NOT NULL DEFAULT '',
    last_gate      jsonb,
    evaluated_at   timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, version)
);
CREATE INDEX releases_project ON releases (project_id, created_at DESC);
