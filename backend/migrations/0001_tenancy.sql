-- 0001_tenancy — tables owned by internal/tenancy (SPEC §6.9.5, §8.2).
--
-- Organization -> Workspace -> Project -> Environment. Run is NOT a level
-- here: it is data, not a permission boundary (§8.8). No ON DELETE CASCADE:
-- deleting a tenant must be a deliberate process that also handles artifacts
-- and retention (§11), never a side effect of one DELETE.

CREATE TABLE organizations (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    slug       text        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(slug) <= 64),
    plan_id    text        NOT NULL DEFAULT 'free',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE workspaces (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid        NOT NULL REFERENCES organizations (id),
    name       text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

CREATE TABLE projects (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   uuid        NOT NULL REFERENCES workspaces (id),
    name           text        NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    default_branch text        NOT NULL DEFAULT 'main',
    created_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

-- Same test, different environment = separate history (§8.2). The key is
-- what the reporter sends as environmentKey (§7.3); ingest creates it on
-- first sight, so it needs the same shape rule as a slug.
CREATE TABLE environments (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid        NOT NULL REFERENCES projects (id),
    key        text        NOT NULL CHECK (key ~ '^[a-z0-9]+(-[a-z0-9]+)*$' AND length(key) <= 64),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, key)
);
