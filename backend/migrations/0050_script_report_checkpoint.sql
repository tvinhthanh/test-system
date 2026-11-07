-- Owned by autotest: keep a completed sandbox report only while its
-- idempotent JUnit import is being retried. This prevents a transient results
-- plane failure from discarding an already completed script execution.
ALTER TABLE script_runs
    ADD COLUMN report bytea CHECK (octet_length(report) <= 8388608);

