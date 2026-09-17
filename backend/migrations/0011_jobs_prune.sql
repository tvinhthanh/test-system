-- 0011_jobs_prune — internal/platform/queue (§6.7.4, §14.1 T5 "không phình queue").
--
-- Finished jobs were kept forever: one row per run, and the api's backlog
-- gauges counted over all of them at every scrape. The queue now prunes
-- done jobs after PruneAfter (Prune, called from the worker sweep) and
-- reads its counts through one partial index per status. Two indexes were
-- missing for that: dead jobs (few, must be fast to count) and the prune
-- scan itself (done jobs by finish time).

CREATE INDEX jobs_dead ON jobs (id) WHERE status = 'dead';
CREATE INDEX jobs_done_finished ON jobs (finished_at) WHERE status = 'done';
