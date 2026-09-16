-- 0005_jobs — the table behind internal/platform/queue (§6.7.4, §6.9.6).
--
-- One table, many logical queues (kind). Claiming uses FOR UPDATE SKIP
-- LOCKED (§6.7.8: this is what runs until p99 claim latency > 2 s at real
-- load). Enqueue joins the caller's transaction, which is the outbox
-- pattern of §6.9.6 without a second table: the job row commits with the
-- business write or not at all, so "run.finished" cannot be lost between
-- the finish handler and the worker (§14.3: kill worker mid-job → no lost
-- results).

CREATE TABLE jobs (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    kind         text        NOT NULL CHECK (length(kind) BETWEEN 1 AND 64),
    key          text,                                              -- dedupe within kind while not finished
    payload      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    status       text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'done', 'dead')),
    attempts     integer     NOT NULL DEFAULT 0,
    max_attempts integer     NOT NULL DEFAULT 5 CHECK (max_attempts >= 1),
    run_at       timestamptz NOT NULL DEFAULT now(),                -- not before; backoff moves it
    leased_until timestamptz,                                       -- a running job past this is claimable again
    last_error   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz
);
-- A key is unique per kind while the job is live: enqueuing "resolve run X"
-- twice yields one job (§14.3 duplicate-safe).
CREATE UNIQUE INDEX jobs_live_key ON jobs (kind, key) WHERE key IS NOT NULL AND status IN ('pending', 'running');
-- The claim query: oldest runnable job of a kind.
CREATE INDEX jobs_claim ON jobs (kind, run_at) WHERE status = 'pending';
-- Lease expiry sweep and queue-depth metrics.
CREATE INDEX jobs_running ON jobs (leased_until) WHERE status = 'running';
