-- Owned by urlcheck. One row per URL check a tester asked for (QĐ-14): the
-- address, who asked, and — once the worker ran it through the checker
-- service — its report and two JPEG screenshots (desktop, phone). queued →
-- done, or error when the checker could not be reached after the job's
-- retries. environment_id is informational (tenancy's table, like plans').
-- The worker prunes rows after 90 days; a project purge removes them.
CREATE TABLE url_checks (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid        NOT NULL,
    environment_id uuid,
    url            text        NOT NULL CHECK (url ~ '^https?://[^/?#\s]+' AND octet_length(url) <= 2048),
    status         text        NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'done', 'error')),
    verdict        text        CHECK (verdict IN ('passed', 'warning', 'failed')),
    requested_by   text        NOT NULL CHECK (length(requested_by) BETWEEN 1 AND 254),
    report         jsonb       CHECK (octet_length(report::text) <= 262144),
    error          text        CHECK (octet_length(error) <= 1000),
    desktop_shot   bytea       CHECK (octet_length(desktop_shot) <= 3145728),
    mobile_shot    bytea       CHECK (octet_length(mobile_shot) <= 3145728),
    created_at     timestamptz NOT NULL DEFAULT now(),
    finished_at    timestamptz,
    CHECK ((status = 'done') = (verdict IS NOT NULL AND report IS NOT NULL)),
    CHECK ((status = 'queued') = (finished_at IS NULL))
);
CREATE INDEX url_checks_project ON url_checks (project_id, created_at DESC, id DESC);
CREATE INDEX url_checks_queued ON url_checks (project_id) WHERE status = 'queued';
CREATE INDEX url_checks_age ON url_checks (created_at);
