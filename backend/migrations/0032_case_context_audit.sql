-- 0032_case_context_audit — tables and trigger owned by internal/testmgmt.
ALTER TABLE test_cases
    ADD COLUMN preconditions text NOT NULL DEFAULT '' CHECK (octet_length(preconditions) <= 16000),
    ADD COLUMN test_data text NOT NULL DEFAULT '' CHECK (octet_length(test_data) <= 16000);

CREATE TABLE test_case_audit (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    test_case_id uuid NOT NULL REFERENCES test_cases(id) ON DELETE CASCADE,
    actor text NOT NULL,
    action text NOT NULL CHECK (action IN ('case.create', 'case.update', 'case.import')),
    changed_fields text[] NOT NULL,
    before_values jsonb,
    after_values jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX test_case_audit_history ON test_case_audit(test_case_id, created_at DESC, id DESC);

CREATE FUNCTION audit_test_case_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    previous jsonb;
    current_values jsonb;
    fields text[];
    actor_name text;
    action_name text;
BEGIN
    current_values := to_jsonb(NEW) - ARRAY['created_at', 'updated_at'];
    IF TG_OP = 'UPDATE' THEN
        previous := to_jsonb(OLD) - ARRAY['created_at', 'updated_at'];
    END IF;
    SELECT array_agg(key ORDER BY key) INTO fields
    FROM jsonb_each(current_values)
    WHERE key NOT IN ('id', 'project_id') AND value IS DISTINCT FROM previous->key;
    IF fields IS NULL THEN RETURN NEW; END IF;
    actor_name := coalesce(nullif(current_setting('app.testmgmt_actor', true), ''), 'system');
    action_name := CASE WHEN current_setting('app.testmgmt_action', true) = 'case.import' THEN 'case.import'
                        WHEN TG_OP = 'INSERT' THEN 'case.create' ELSE 'case.update' END;
    INSERT INTO test_case_audit(project_id, test_case_id, actor, action, changed_fields, before_values, after_values)
    VALUES (NEW.project_id, NEW.id, actor_name, action_name, fields, previous, current_values);
    RETURN NEW;
END;
$$;

CREATE TRIGGER test_case_change_audit AFTER INSERT OR UPDATE ON test_cases
FOR EACH ROW EXECUTE FUNCTION audit_test_case_change();

-- Do not backfill audit or execution snapshots: past edits have no recorded actor.
