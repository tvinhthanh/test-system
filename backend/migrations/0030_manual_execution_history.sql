ALTER TABLE manual_executions
    ADD COLUMN case_snapshot jsonb,
    ADD COLUMN step_results jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN bug_url text NOT NULL DEFAULT '',
    ADD COLUMN retest_of uuid REFERENCES manual_executions(id);

-- Old executions deliberately remain NULL: current cases cannot reconstruct history.
ALTER TABLE manual_executions ADD CONSTRAINT manual_execution_snapshot_object
    CHECK (case_snapshot IS NULL OR jsonb_typeof(case_snapshot) = 'object');
ALTER TABLE manual_executions ADD CONSTRAINT manual_execution_steps_array
    CHECK (jsonb_typeof(step_results) = 'array');
CREATE INDEX manual_executions_plan_history
    ON manual_executions (test_plan_id, executed_at DESC, id DESC);
