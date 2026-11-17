-- Owned by testmgmt; deliberately separate from Run/Attempt/flaky/TTRC.
ALTER TABLE test_cases ADD CONSTRAINT test_case_project_identity UNIQUE(project_id, id);
CREATE TABLE specialized_results (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    test_case_id uuid,
    kind text NOT NULL CHECK (kind IN ('performance','security','accessibility','compatibility','recovery','usability')),
    title text NOT NULL CHECK (octet_length(title) BETWEEN 1 AND 500),
    outcome text NOT NULL CHECK (outcome IN ('passed','failed','inconclusive','not_run','not_applicable','import_error')),
    envelope jsonb NOT NULL,
    original_report text NOT NULL CHECK (octet_length(original_report) <= 524288),
    idempotency_key text NOT NULL CHECK (octet_length(idempotency_key) BETWEEN 1 AND 128),
    payload_hash text NOT NULL,
    retest_of uuid,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE(project_id, id),
    UNIQUE(project_id, idempotency_key),
    FOREIGN KEY(project_id, test_case_id) REFERENCES test_cases(project_id, id) ON DELETE CASCADE,
    FOREIGN KEY(project_id, retest_of) REFERENCES specialized_results(project_id, id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX specialized_results_project ON specialized_results(project_id, created_at DESC, id DESC);
CREATE FUNCTION prevent_specialized_result_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'specialized results are append-only' USING ERRCODE = '55000';
END;
$$;
CREATE TRIGGER specialized_result_append_only BEFORE UPDATE ON specialized_results
FOR EACH ROW EXECUTE FUNCTION prevent_specialized_result_update();
