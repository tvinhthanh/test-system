-- Owned by urlcheck (QĐ-16): a site scan is a URL check of the start page
-- and of every page reached from it by same-site links, breadth first, up
-- to max_pages (and three links deep). Each page is an ordinary url_checks
-- row with the scan's id; the scan is done when none of its pages is left
-- queued. account: an environment test account (autotest's) to scan as a
-- signed-in user; links that sign out or delete are never followed.
CREATE TABLE site_scans (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     uuid        NOT NULL,
    environment_id uuid,
    start_url      text        NOT NULL CHECK (start_url ~ '^https?://[^/?#\s]+' AND octet_length(start_url) <= 2048),
    max_pages      integer     NOT NULL CHECK (max_pages BETWEEN 1 AND 50),
    account        text        NOT NULL DEFAULT '' CHECK (length(account) <= 40),
    status         text        NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'done', 'error')),
    requested_by   text        NOT NULL CHECK (length(requested_by) BETWEEN 1 AND 254),
    error          text        CHECK (octet_length(error) <= 1000),
    created_at     timestamptz NOT NULL DEFAULT now(),
    finished_at    timestamptz,
    CHECK ((status = 'running') = (finished_at IS NULL))
);
CREATE INDEX site_scans_project ON site_scans (project_id, created_at DESC, id DESC);
CREATE INDEX site_scans_age ON site_scans (created_at);

ALTER TABLE url_checks
    ADD COLUMN scan_id uuid REFERENCES site_scans (id) ON DELETE CASCADE,
    ADD COLUMN depth   integer NOT NULL DEFAULT 0 CHECK (depth BETWEEN 0 AND 3);
CREATE INDEX url_checks_scan ON url_checks (scan_id) WHERE scan_id IS NOT NULL;
-- A scan's pages do not count toward a project's waiting checks (MaxQueued).
DROP INDEX url_checks_queued;
CREATE INDEX url_checks_queued ON url_checks (project_id) WHERE status = 'queued' AND scan_id IS NULL;
-- The same page once per scan, whichever link reached it first.
CREATE UNIQUE INDEX url_checks_scan_url ON url_checks (scan_id, url) WHERE scan_id IS NOT NULL;
