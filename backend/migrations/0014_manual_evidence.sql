-- 0014_manual_evidence — table owned by internal/artifact (SPEC §6.8.2, §8.4, §8.10).
--
-- "Artifact dùng lại, không tạo bảng mới": evidence a person attaches to a
-- manual execution (a screenshot, a hand-recorded video) is an Artifact of
-- §8.4 with kind = 'manual_evidence'. §8.10 makes adding the enum value a
-- reviewed migration, in P2, not a quiet P1 change — this is that file.
--
-- A manual execution is not a Run (§6.8.6), so this kind has no run, no
-- clientTestId and no attempt: those three go nullable and a CHECK ties
-- them to the kind, so the reporter's kinds still carry all of them and the
-- manual kind never does. manual_execution_id is a plain uuid, not a FOREIGN
-- KEY into testmgmt's table (§6.9.5: no cross-module FK); cmd/api resolves
-- the execution through testmgmt's contract before the artifact is written.
--
-- Evidence never links to an attempt, so the unlinked sweep and its index
-- (§7.6 #3, §6.4 "unlinked > 1 h") must not see it: both now require run_id.

ALTER TABLE artifacts ALTER COLUMN run_id DROP NOT NULL;
ALTER TABLE artifacts ALTER COLUMN client_test_id DROP NOT NULL;
ALTER TABLE artifacts ALTER COLUMN attempt DROP NOT NULL;
ALTER TABLE artifacts ADD COLUMN manual_execution_id uuid;

ALTER TABLE artifacts DROP CONSTRAINT artifacts_kind_check;
ALTER TABLE artifacts ADD CONSTRAINT artifacts_kind_check
    CHECK (kind IN ('screenshot', 'video', 'trace', 'har', 'log', 'attachment', 'manual_evidence'));

ALTER TABLE artifacts ADD CONSTRAINT artifacts_owner_check CHECK (
    (kind = 'manual_evidence'
        AND manual_execution_id IS NOT NULL
        AND run_id IS NULL AND client_test_id IS NULL AND attempt IS NULL)
    OR
    (kind <> 'manual_evidence'
        AND manual_execution_id IS NULL
        AND run_id IS NOT NULL AND client_test_id IS NOT NULL AND attempt IS NOT NULL)
);

-- The plan page lists evidence per execution.
CREATE INDEX artifacts_manual_execution ON artifacts (manual_execution_id, created_at) WHERE manual_execution_id IS NOT NULL;

DROP INDEX artifacts_unlinked;
CREATE INDEX artifacts_unlinked ON artifacts (created_at)
    WHERE test_result_attempt_id IS NULL AND run_id IS NOT NULL AND status IN ('pending', 'uploaded', 'verified');
