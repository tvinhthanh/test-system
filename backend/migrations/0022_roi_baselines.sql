-- 0022_roi_baselines — owned by internal/testmgmt (SPEC §6.8.3, QĐ-13).
--
-- ROI is "giờ triage tiết kiệm" against a TTRC baseline that was measured
-- (§5.4), never assumed: without a row here the ROI screen says "not
-- enough data". A baseline is what §5.4's output table holds for one
-- partner/project — n failures, median and p90 TTRC, % rerun-to-pass —
-- labelled retrospective or prospective, with who entered it. Append-only:
-- a correction is a new row, the old one stays for the record; the newest
-- prospective row applies, else the newest retrospective (§5.4: prospective
-- is the official number).

CREATE TABLE ttrc_baselines (
    id                           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id                   uuid        NOT NULL,
    kind                         text        NOT NULL CHECK (kind IN ('retrospective', 'prospective')),
    failures                     integer     NOT NULL CHECK (failures > 0),
    median_seconds               integer     NOT NULL CHECK (median_seconds > 0),
    p90_seconds                  integer     CHECK (p90_seconds IS NULL OR p90_seconds >= median_seconds),
    rerun_pass_rate              numeric(5,4) CHECK (rerun_pass_rate IS NULL OR rerun_pass_rate BETWEEN 0 AND 1),
    -- Median time a person spent investigating a failure that turned out
    -- flaky, measured in the same study; §6.8.3's "thời gian điều tra trung
    -- vị". NULL: not measured, and the flaky-hours figure leaves that term out.
    flaky_investigation_seconds  integer     CHECK (flaky_investigation_seconds IS NULL OR flaky_investigation_seconds > 0),
    period_from                  date        NOT NULL,
    period_to                    date        NOT NULL CHECK (period_to >= period_from),
    note                         text        NOT NULL DEFAULT '' CHECK (length(note) <= 2000),
    entered_by                   text        NOT NULL CHECK (length(entered_by) BETWEEN 1 AND 320),
    entered_at                   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ttrc_baselines_project ON ttrc_baselines (project_id, entered_at DESC);
