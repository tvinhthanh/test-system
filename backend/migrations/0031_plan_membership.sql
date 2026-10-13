-- 0031_plan_membership — tables owned by internal/testmgmt.
CREATE TABLE test_plan_cases (
    test_plan_id uuid NOT NULL REFERENCES test_plans(id) ON DELETE CASCADE,
    test_case_id uuid NOT NULL REFERENCES test_cases(id) ON DELETE CASCADE,
    project_id uuid NOT NULL,
    assignee_user_id uuid,
    assignee_email text NOT NULL DEFAULT '',
    assigned_by text NOT NULL DEFAULT '',
    assigned_at timestamptz,
    work_status text NOT NULL DEFAULT 'not_run'
        CHECK (work_status IN ('not_run', 'in_progress', 'completed')),
    PRIMARY KEY (test_plan_id, test_case_id)
);
CREATE INDEX test_plan_cases_project ON test_plan_cases(project_id, test_plan_id);

-- Freeze legacy scope, including obsolete cases that already carry evidence.
INSERT INTO test_plan_cases(test_plan_id, test_case_id, project_id, work_status)
SELECT p.id, c.id, p.project_id,
       CASE WHEN EXISTS (SELECT 1 FROM manual_executions e
                         WHERE e.test_plan_id = p.id AND e.test_case_id = c.id)
            THEN 'completed' ELSE 'not_run' END
FROM test_plans p JOIN test_cases c ON c.project_id = p.project_id
WHERE c.automation_status <> 'obsolete'
   OR EXISTS (SELECT 1 FROM manual_executions e
              WHERE e.test_plan_id = p.id AND e.test_case_id = c.id);

CREATE TABLE test_plan_audit (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid NOT NULL,
    test_plan_id uuid NOT NULL REFERENCES test_plans(id) ON DELETE CASCADE,
    actor text NOT NULL,
    action text NOT NULL CHECK (action IN ('plan.create', 'plan.scope', 'plan.assignment', 'plan.workflow', 'plan.status', 'plan.execution')),
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX test_plan_audit_plan ON test_plan_audit(test_plan_id, created_at DESC);
