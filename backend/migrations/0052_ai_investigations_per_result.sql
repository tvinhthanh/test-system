-- Owned by triage (QĐ-4): a same-day investigation is deduped per result,
-- not per project. With the old key, a second result of the same test
-- failing the same way that day got the first result's investigation back,
-- and its own page (which lists investigations by result) showed nothing.
-- Each result now gets its own row; the worker copies a finished sibling's
-- conclusion (same dedupe key, same day) instead of paying for it again.
ALTER TABLE ai_investigations DROP CONSTRAINT ai_investigations_project_id_dedupe_key_requested_day_key;
ALTER TABLE ai_investigations ADD CONSTRAINT ai_investigations_result_dedupe UNIQUE (project_id, test_result_id, dedupe_key, requested_day);
CREATE INDEX ai_investigations_shared ON ai_investigations (project_id, dedupe_key, requested_day) WHERE status = 'done';
