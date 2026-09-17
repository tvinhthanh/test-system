-- 0010_flaky_incremental — §8.6 says "incremental, không full scan", and
-- 0007 was not: every run finish re-read the whole 30-day history of every
-- identity in the run. At 1 000 runs × 1 000 tests a month that is a
-- million rows per finish (test/load, §14.2 — the queue never drained).
--
-- The signal row now carries the fold state the formula needs to take one
-- more result without the history (last_outcome), and the time it was
-- last rebuilt exactly from the window (rebuilt_at). A finish applies its
-- own results as a delta; a sweep rebuilds rows older than a day so results
-- that left the window are dropped.
ALTER TABLE flaky_signals
    ADD COLUMN last_outcome smallint    NOT NULL DEFAULT 0 CHECK (last_outcome IN (-1, 0, 1)),  -- 1 pass, -1 fail, 0 none
    ADD COLUMN rebuilt_at   timestamptz NOT NULL DEFAULT now();
CREATE INDEX flaky_signals_rebuilt ON flaky_signals (rebuilt_at);

-- Failures in a window, project-wide (triage metrics, category trend):
-- the window is the selective predicate, and only failed results carry it.
CREATE INDEX test_results_failure_observed ON test_results (failure_observed_at) WHERE failure_observed_at IS NOT NULL;
