-- 0006_artifacts — table owned by internal/artifact (SPEC §6.9.5, §8.4, §7.7, §10.4).
--
-- Attribution is per attempt (§7.7 invariant): the four protocol keys
-- (run, clientTestId, occurrenceIndex, attempt) plus name identify an
-- artifact, and that natural key is what makes POST /v1/artifacts
-- idempotent (§7.2 #4) — re-declaring returns the same row and fresh URLs.
-- test_result_attempt_id is filled by linking, which may happen after the
-- artifact arrives (§7.6 #3: out-of-order tolerant); neither it nor run_id
-- carries a FOREIGN KEY into results' tables (§6.9.5).
--
-- Status adds `rejected` to §8.4's list (size or checksum mismatch at
-- complete; the object is deleted). §8.10: added by migration, old values
-- keep their meaning.

CREATE TABLE artifacts (
    id                     uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id             uuid        NOT NULL,
    run_id                 uuid        NOT NULL,
    test_result_attempt_id uuid,
    client_test_id         text        NOT NULL CHECK (length(client_test_id) BETWEEN 1 AND 256),
    occurrence_index       integer     NOT NULL DEFAULT 0 CHECK (occurrence_index >= 0),
    attempt                integer     NOT NULL CHECK (attempt >= 0),
    kind                   text        NOT NULL CHECK (kind IN ('screenshot', 'video', 'trace', 'har', 'log', 'attachment')),
    name                   text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    content_type           text        NOT NULL,
    declared_bytes         bigint      NOT NULL CHECK (declared_bytes > 0),
    actual_bytes           bigint,
    sha256                 text        NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    storage_key            text        NOT NULL UNIQUE,                 -- server-generated; the client never chooses it (§7.7)
    upload_id              text,                                        -- multipart in progress
    status                 text        NOT NULL DEFAULT 'pending'
                           CHECK (status IN ('pending', 'uploaded', 'verified', 'rejected', 'expired', 'deleted')),
    failure_reason         text,                                        -- SIZE_MISMATCH, CHECKSUM_MISMATCH, ABANDONED, ...
    expires_at             timestamptz NOT NULL,                        -- retention, fixed at declare time (§8.4)
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    verified_at            timestamptz,
    linked_at              timestamptz,
    UNIQUE (run_id, client_test_id, occurrence_index, attempt, name)
);
-- §8.9 retention sweep.
CREATE INDEX artifacts_expiry ON artifacts (expires_at) WHERE status = 'verified';
-- §6.4 "unlinked > 1 h" and the linking sweep.
CREATE INDEX artifacts_unlinked ON artifacts (created_at) WHERE test_result_attempt_id IS NULL AND status IN ('pending', 'uploaded', 'verified');
-- §7.7 abandoned-upload cleanup (24 h).
CREATE INDEX artifacts_pending ON artifacts (created_at) WHERE status = 'pending';
CREATE INDEX artifacts_run ON artifacts (run_id);
