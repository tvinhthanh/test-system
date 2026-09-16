-- 0003_results — tables owned by internal/results (SPEC §6.9.5, §8.4).
--
-- Run / TestResult / TestResultAttempt, plus run_events: the set of event
-- sequences a run has already accepted, which is what makes
-- POST /v1/runs/{localRunId}/events idempotent on (localRunId, sequence)
-- (§7.2) without a general idempotency store.
--
-- runs carries project_id next to environment_id on purpose. §8.4 keys the
-- run on (environmentId, localRunId); the protocol, however, addresses a run
-- by localRunId alone under a project-scoped token (§7.2), so the lookup key
-- is (project_id, local_run_id) and the tenant condition lives in the query
-- (§11.3.3) without joining tenancy's tables (§6.9.5). Both are references
-- into another module and therefore carry no FOREIGN KEY.

CREATE TABLE runs (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid        NOT NULL,
    environment_id uuid        NOT NULL,
    local_run_id   uuid        NOT NULL,
    status         text        NOT NULL CHECK (status IN ('queued', 'running', 'passed', 'failed', 'error', 'cancelled')),
    flaky_count    integer     NOT NULL DEFAULT 0,                 -- QĐ-7 B: count of final_status = flaky_pass
    error_reason   text,                                           -- RUN_TIMED_OUT, AGENT_LOST (P3)
    trigger        jsonb       NOT NULL DEFAULT '{}'::jsonb,       -- stored and rendered, never fetched (§7.10)
    commit_sha     text,
    branch         text,
    framework      jsonb       NOT NULL DEFAULT '{}'::jsonb,       -- {name, version}
    reporter       jsonb       NOT NULL DEFAULT '{}'::jsonb,       -- {name, version, protocolVersion}: §9.5 needs it
    started_at     timestamptz NOT NULL,
    finished_at    timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, local_run_id)
);
-- §8.9: run list per environment.
CREATE INDEX runs_environment_started ON runs (environment_id, started_at DESC);
-- Auto-close sweep (§7.4: open > 6 h → error RUN_TIMED_OUT).
CREATE INDEX runs_open ON runs (started_at) WHERE status = 'running';

CREATE TABLE run_events (
    run_id   uuid   NOT NULL REFERENCES runs (id),
    sequence bigint NOT NULL,
    PRIMARY KEY (run_id, sequence)
);

CREATE TABLE test_results (
    id                  uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id              uuid        NOT NULL REFERENCES runs (id),
    stable_test_id      uuid,                                      -- backfilled by identity (§7.6); no FK across modules
    client_test_id      text        NOT NULL CHECK (length(client_test_id) BETWEEN 1 AND 256),
    occurrence_index    integer     NOT NULL DEFAULT 0 CHECK (occurrence_index >= 0),   -- N03
    display_name        text        NOT NULL,
    suite_name          text        NOT NULL DEFAULT '',
    identity_raw        jsonb       NOT NULL,                      -- {framework, filePath, titlePath, paramSignature}: tier-2 resolve input
    final_status        text        NOT NULL CHECK (final_status IN ('passed', 'failed', 'skipped', 'timed_out', 'interrupted', 'flaky_pass')),
    failure_category    text,                                      -- §8.7, set by triage
    attempt_count       integer     NOT NULL DEFAULT 1,
    first_failed_at     timestamptz,                               -- client clock, forensic
    failure_observed_at timestamptz,                               -- server clock, TTRC origin (§8.3); set once
    duration_ms         integer     NOT NULL DEFAULT 0,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (run_id, client_test_id, occurrence_index)              -- N03
);
-- §8.9: history of one test; §8.4: client_test_id indexed.
CREATE INDEX test_results_stable_created ON test_results (stable_test_id, created_at DESC);
CREATE INDEX test_results_client ON test_results (client_test_id);
-- Tier-2 resolve at run.finish walks the unresolved results of one run.
CREATE INDEX test_results_run_unresolved ON test_results (run_id) WHERE stable_test_id IS NULL;

CREATE TABLE test_result_attempts (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    test_result_id    uuid        NOT NULL REFERENCES test_results (id),
    attempt           integer     NOT NULL CHECK (attempt >= 0),   -- 0-based (§7.3)
    status            text        NOT NULL CHECK (status IN ('passed', 'failed', 'skipped', 'timed_out', 'interrupted')),
    framework_outcome text        NOT NULL CHECK (framework_outcome IN ('expected', 'unexpected', 'flaky', 'skipped')),  -- N36/N41: carried, never re-derived
    error             jsonb,                                       -- {message, stack, snippet, truncated}
    worker_id         text,
    agent_id          uuid,                                        -- P3
    started_at        timestamptz,
    finished_at       timestamptz,
    duration_ms       integer     NOT NULL DEFAULT 0,
    created_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (test_result_id, attempt)
);
