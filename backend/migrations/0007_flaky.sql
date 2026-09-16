-- 0007_flaky — table owned by internal/flaky (SPEC §6.9.5, §8.6).
--
-- One row per (identity, environment): the current flaky verdict, rebuilt
-- from the last window_days of results every time a run of that identity
-- finishes (incremental: only identities in the finished run, never a full
-- scan). identity_id, environment_id and project_id are references into
-- other modules' tables and carry no FOREIGN KEY (§6.9.5).

CREATE TABLE flaky_signals (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      uuid        NOT NULL,
    identity_id     uuid        NOT NULL,
    environment_id  uuid        NOT NULL,
    window_days     integer     NOT NULL DEFAULT 30,
    total_runs      integer     NOT NULL DEFAULT 0,
    failed_runs     integer     NOT NULL DEFAULT 0,
    flaky_pass_runs integer     NOT NULL DEFAULT 0,
    flip_count      integer     NOT NULL DEFAULT 0,
    flaky_score     numeric(4, 3),                                  -- NULL until total_runs >= 2 (N05)
    verdict         text        NOT NULL CHECK (verdict IN ('insufficient_data', 'stable', 'suspect', 'flaky', 'quarantine_suggested')),
    last_failed_at  timestamptz,
    computed_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (identity_id, environment_id)
);
-- Dashboard: flaky tests of a project, worst first.
CREATE INDEX flaky_signals_project_score ON flaky_signals (project_id, flaky_score DESC NULLS LAST);
