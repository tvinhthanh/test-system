-- 0015_rbac_six_roles — table owned by internal/authz (SPEC §11.3.1, §4.4 "RBAC 6 role — P2 mở rộng").
--
-- P1-min stored three roles (admin / member / viewer, 0002). This unfolds
-- them into the six of §11.3.1, each attached at exactly one level of the
-- tree ("Gán ở tầng") and inherited downwards; a second CHECK ties role to
-- scope_level so a grant at the wrong level is refused by the database as
-- well as by GrantRole.
--
-- Existing rows: `admin` meant "the strongest role at this level", so it
-- becomes org_owner at an organization (P1-min's org admin held org.manage,
-- which only the owner has now), workspace_admin at a workspace and
-- maintainer at a project. member/viewer rows above project level are
-- expanded to one row per project under that node — the single read of
-- tenancy's tables in this file, a one-time data move, not a dependency —
-- and then removed. A person who already had a project-level row keeps it
-- (ON CONFLICT DO NOTHING): the more specific grant wins.

-- Order matters: the old CHECK goes first (the new names violate it), the
-- rows are renamed, and only then does the new CHECK go on — a CHECK is
-- validated against every row the moment it is added.
ALTER TABLE memberships DROP CONSTRAINT memberships_role_check;

UPDATE memberships SET role = 'org_owner'       WHERE role = 'admin' AND scope_level = 'organization';
UPDATE memberships SET role = 'workspace_admin' WHERE role = 'admin' AND scope_level = 'workspace';
UPDATE memberships SET role = 'maintainer'      WHERE role = 'admin' AND scope_level = 'project';

ALTER TABLE memberships ADD CONSTRAINT memberships_role_check
    CHECK (role IN ('org_owner', 'org_admin', 'workspace_admin', 'maintainer', 'member', 'viewer'));

INSERT INTO memberships (user_id, scope_level, scope_id, role)
SELECT m.user_id, 'project', p.id, m.role
  FROM memberships m
  JOIN workspaces w ON (m.scope_level = 'workspace'    AND w.id = m.scope_id)
                    OR (m.scope_level = 'organization' AND w.org_id = m.scope_id)
  JOIN projects p ON p.workspace_id = w.id
 WHERE m.role IN ('member', 'viewer') AND m.scope_level <> 'project'
ON CONFLICT (user_id, scope_level, scope_id) DO NOTHING;
DELETE FROM memberships WHERE role IN ('member', 'viewer') AND scope_level <> 'project';

ALTER TABLE memberships ADD CONSTRAINT memberships_role_level_check CHECK (
    (role IN ('org_owner', 'org_admin')          AND scope_level = 'organization')
    OR (role = 'workspace_admin'                  AND scope_level = 'workspace')
    OR (role IN ('maintainer', 'member', 'viewer') AND scope_level = 'project')
);
