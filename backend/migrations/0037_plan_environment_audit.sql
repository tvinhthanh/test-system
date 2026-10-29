-- Owned by testmgmt. A plan's environment (and so the address its testers
-- open, QĐ-14) can change after creation; that change is audited like the
-- plan's scope and status.
ALTER TABLE test_plan_audit DROP CONSTRAINT test_plan_audit_action_check;
ALTER TABLE test_plan_audit ADD CONSTRAINT test_plan_audit_action_check
    CHECK (action IN ('plan.create', 'plan.scope', 'plan.assignment', 'plan.workflow', 'plan.status', 'plan.execution', 'plan.environment'));
