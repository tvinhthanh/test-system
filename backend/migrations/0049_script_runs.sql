-- Owned by autotest (QĐ-19 tier 4): a team's own test scripts — a
-- Playwright project or a k6 script — run by sandboxd in a throwaway
-- container that reaches only the environment's host.
--
-- archive is the upload (.tar.gz, at most 10 MB); it is cleared when the
-- run ends, so a script is kept no longer than it takes to run. A k6 run
-- sends load, so like a load run it needs the organization's proof that it
-- owns the host (site_ownership, 0048). The report goes through the JUnit
-- import into a test run (test_run_id, results' — no FK across modules).
CREATE TABLE script_runs (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      uuid        NOT NULL,
    org_id          uuid        NOT NULL,
    environment_id  uuid        NOT NULL,
    kind            text        NOT NULL CHECK (kind IN ('playwright', 'k6')),
    base            text        NOT NULL CHECK (octet_length(base) <= 2048),
    entry           text        NOT NULL CHECK (length(entry) BETWEEN 1 AND 200),
    project         text        NOT NULL DEFAULT '' CHECK (length(project) <= 100),
    grep            text        NOT NULL DEFAULT '' CHECK (length(grep) <= 100),
    vus             integer     CHECK (vus BETWEEN 1 AND 200),
    seconds         integer     CHECK (seconds BETWEEN 5 AND 600),
    timeout_seconds integer     NOT NULL CHECK (timeout_seconds BETWEEN 30 AND 3600),
    archive         bytea       CHECK (octet_length(archive) <= 10485760),
    archive_bytes   integer     NOT NULL CHECK (archive_bytes > 0),
    status          text        NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'error')),
    verdict         text        NOT NULL DEFAULT '' CHECK (verdict IN ('', 'passed', 'failed')),
    exit_code       integer,
    timed_out       boolean     NOT NULL DEFAULT false,
    tests           integer     NOT NULL DEFAULT 0,
    failures        integer     NOT NULL DEFAULT 0,
    log             text        NOT NULL DEFAULT '' CHECK (octet_length(log) <= 70000),
    error           text        NOT NULL DEFAULT '' CHECK (octet_length(error) <= 1000),
    test_run_id     uuid,
    requested_by    text        NOT NULL CHECK (length(requested_by) BETWEEN 1 AND 254),
    created_at      timestamptz NOT NULL DEFAULT now(),
    started_at      timestamptz,
    finished_at     timestamptz,
    CHECK ((status IN ('queued', 'running')) = (finished_at IS NULL)),
    CHECK (status IN ('queued', 'running') OR archive IS NULL),
    CHECK ((kind = 'k6') = (vus IS NOT NULL AND seconds IS NOT NULL))
);
CREATE INDEX script_runs_project ON script_runs (project_id, created_at DESC, id DESC);
CREATE INDEX script_runs_active ON script_runs (org_id) WHERE status IN ('queued', 'running');
CREATE INDEX script_runs_age ON script_runs (created_at);
