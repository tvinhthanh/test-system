-- 0016_members — member management from the dashboard (§11.3.2 "Quản lý
-- member (tầng tương ứng)", P2).
--
-- Until now roles were granted only by cmd/admin, so authz_audit recorded
-- revocations alone (§11.7 logs destructive actions). A grant through the
-- api can also demote — re-granting a lower role on the same node replaces
-- the row — so every grant made by a person is recorded too, with the role
-- it replaced.
ALTER TABLE authz_audit DROP CONSTRAINT authz_audit_action_check;
ALTER TABLE authz_audit ADD CONSTRAINT authz_audit_action_check
    CHECK (action IN ('token.revoke', 'role.revoke', 'role.grant'));

-- The member list reads every membership on a project's chain by node;
-- memberships_user_scope leads with user_id and cannot serve it.
CREATE INDEX memberships_scope ON memberships (scope_id, scope_level);
