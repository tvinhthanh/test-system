-- 0023_meter_daily — owned by internal/billing (SPEC §10.5–10.6, QĐ-13).
--
-- The shadow meter: one row per project per UTC day, counted, never
-- charged. §10.6 asks for four weeks of it before any real billing: the
-- trial axis (active tests = stableTestIds with a result in 30 days), the
-- raw distinct client fingerprints it is checked against, and the identity
-- corrections (merge, undo, split, review decisions) that can move it. If
-- corrections move the meter more than 2 % away from the raw count, the
-- active-test count is not a billing meter. Storage and volume ride along
-- for the §10.5 quotas. Rewriting a day (a re-run of the snapshot) replaces
-- it; no invoice ever reads this table (§10.5: no retroactive bills).

CREATE TABLE meter_daily (
    project_id        uuid        NOT NULL,
    org_id            uuid        NOT NULL,
    day               date        NOT NULL,
    active_tests      integer     NOT NULL CHECK (active_tests >= 0),
    raw_fingerprints  integer     NOT NULL CHECK (raw_fingerprints >= 0),
    merges            integer     NOT NULL DEFAULT 0 CHECK (merges >= 0),
    undo_merges       integer     NOT NULL DEFAULT 0 CHECK (undo_merges >= 0),
    splits            integer     NOT NULL DEFAULT 0 CHECK (splits >= 0),
    review_decisions  integer     NOT NULL DEFAULT 0 CHECK (review_decisions >= 0),
    runs              integer     NOT NULL DEFAULT 0 CHECK (runs >= 0),
    results           integer     NOT NULL DEFAULT 0 CHECK (results >= 0),
    artifact_bytes    bigint      NOT NULL DEFAULT 0 CHECK (artifact_bytes >= 0),
    recorded_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, day)
);
CREATE INDEX meter_daily_org_day ON meter_daily (org_id, day);
