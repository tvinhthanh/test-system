-- Owned by autotest (QĐ-19 tier 3): load tests, and the ownership proof
-- they need first.
--
-- An organization proves it owns a host — the exact host of an
-- environment's address — by publishing the token at
-- /.well-known/atp-verification.txt on it, or in a DNS TXT record at
-- _atp-verification.<host>. The worker checks it when asked, and again
-- right before every load run. org_id is tenancy's (no FK, like plans').
CREATE TABLE site_ownership (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid        NOT NULL,
    host        text        NOT NULL CHECK (host ~ '^[a-z0-9.:\[\]-]{1,255}$'),
    token       text        NOT NULL CHECK (token ~ '^[a-z0-9]{32}$'),
    method      text        NOT NULL DEFAULT '' CHECK (method IN ('', 'file', 'dns')),
    status      text        NOT NULL DEFAULT 'unverified' CHECK (status IN ('unverified', 'checking', 'verified', 'failed')),
    error       text        NOT NULL DEFAULT '' CHECK (octet_length(error) <= 1000),
    verified_at timestamptz,
    checked_at  timestamptz,
    created_by  text        NOT NULL CHECK (length(created_by) BETWEEN 1 AND 254),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, host)
);

-- One load test: vus virtual users send GETs to paths on the environment's
-- host for seconds, within the operator's caps (LOAD_MAX_VUS,
-- LOAD_MAX_SECONDS). result is the checker's answer (latency percentiles,
-- statuses, a timeline); verdict compares it with the thresholds.
CREATE TABLE load_runs (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        uuid        NOT NULL,
    org_id            uuid        NOT NULL,
    environment_id    uuid        NOT NULL,
    host              text        NOT NULL,
    base              text        NOT NULL CHECK (octet_length(base) <= 2048),
    paths             text[]      NOT NULL CHECK (cardinality(paths) BETWEEN 1 AND 20),
    vus               integer     NOT NULL CHECK (vus BETWEEN 1 AND 200),
    seconds           integer     NOT NULL CHECK (seconds BETWEEN 5 AND 600),
    ramp_seconds      integer     NOT NULL DEFAULT 0 CHECK (ramp_seconds >= 0 AND ramp_seconds < seconds),
    max_p95_ms        integer     CHECK (max_p95_ms BETWEEN 1 AND 600000),
    max_error_rate    numeric(5,2) CHECK (max_error_rate BETWEEN 0 AND 100),
    status            text        NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'error')),
    verdict           text        NOT NULL DEFAULT '' CHECK (verdict IN ('', 'passed', 'failed')),
    reasons           text[]      NOT NULL DEFAULT '{}',
    result            jsonb       CHECK (octet_length(result::text) <= 262144),
    error             text        CHECK (octet_length(error) <= 1000),
    requested_by      text        NOT NULL CHECK (length(requested_by) BETWEEN 1 AND 254),
    created_at        timestamptz NOT NULL DEFAULT now(),
    started_at        timestamptz,
    finished_at       timestamptz,
    CHECK ((status IN ('queued', 'running')) = (finished_at IS NULL))
);
CREATE INDEX load_runs_project ON load_runs (project_id, created_at DESC, id DESC);
CREATE INDEX load_runs_active ON load_runs (org_id) WHERE status IN ('queued', 'running');
CREATE INDEX load_runs_age ON load_runs (created_at);
