-- 0012_authz_audit — internal/authz (§11.7: "P1 chỉ log hành động phá hủy
-- (xóa, revoke, merge) vào bảng đơn giản").
--
-- identity_audit (0004) already covers merge/undo/split. This is the same
-- shape for authz's destructive operations: revoking an ingest token and
-- removing a membership. Append-only; the row is written in the same
-- transaction as the change, so there is no revoke without a record and no
-- record of a revoke that rolled back. Who did it is a free-text actor
-- (the admin CLI's -actor, later the session user), never a foreign key —
-- the audit must outlive the account that acted.

CREATE TABLE authz_audit (
    id           bigserial   PRIMARY KEY,
    action       text        NOT NULL CHECK (action IN ('token.revoke', 'role.revoke')),
    subject_kind text        NOT NULL CHECK (subject_kind IN ('api_token', 'membership')),
    subject_id   uuid        NOT NULL,
    scope_level  text,                 -- the token's / membership's scope, for filtering by tenant
    scope_id     uuid,
    actor        text        NOT NULL CHECK (length(actor) BETWEEN 1 AND 254),
    details      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX authz_audit_scope ON authz_audit (scope_id, at);
