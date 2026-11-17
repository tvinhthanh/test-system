#!/usr/bin/env bash
# Local dev stack only: use the dashboard as an org owner without enrolling MFA.
#
# The api holds an org owner who has no TOTP at the MFA setup screen (§11.2:
# every other route answers 403 MFA_ENROLLMENT_REQUIRED). On a throwaway local
# stack that is friction, so this turns such owners into org_admin — every
# permission except ActionOrgManage (owners, org delete/restore, usage) — and
# back. Plain SQL in the compose Postgres: no authz_audit row, and the API's
# LAST_OWNER rule is not consulted. Refuses unless deploy/.env has APP_ENV=dev.
#
#   bash server/dev-owner-mfa.sh status
#   bash server/dev-owner-mfa.sh off [email]   owners without MFA → org_admin (all, or that account)
#   bash server/dev-owner-mfa.sh on <email>    that account's org_admin roles → org_owner
#
# Takes effect on the next request: reload the page, no restart or new sign-in.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY="$ROOT/backend/deploy"
ENV_FILE="$DEPLOY/.env"
export MSYS_NO_PATHCONV=1

host_path() { if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else echo "$1"; fi; }
dc() { docker compose -f "$(host_path "$DEPLOY/compose.yml")" --env-file "$(host_path "$ENV_FILE")" "$@"; }
die() { printf '\033[1;31mxx\033[0m %s\n' "$*" >&2; exit 1; }
# Statements come on stdin so :'email' is quoted by psql, never by the shell.
sql() { dc exec -T postgres psql -U atp -d atp -X -q -v ON_ERROR_STOP=1 "$@"; }

[ -f "$ENV_FILE" ] || die "no $ENV_FILE — run: bash server/atp.sh init"
appenv=$(grep -E '^APP_ENV=' "$ENV_FILE" | tail -1 | cut -d= -f2- | tr -d '\r')
[ "$appenv" = dev ] || die "APP_ENV is '${appenv:-unset}', not dev — this script is for a local stack only"

status() {
	sql <<'SQL'
SELECT u.email, m.role, o.name AS organization,
       CASE WHEN u.totp_enabled_at IS NULL THEN 'off' ELSE 'on' END AS mfa
  FROM memberships m
  JOIN users u ON u.id = m.user_id
  JOIN organizations o ON o.id = m.scope_id
 WHERE m.scope_level = 'organization'
 ORDER BY u.email, o.name;
SQL
}

case "${1:-}" in
	status)
		status ;;
	off)
		sql -v email="${2:-}" <<'SQL'
UPDATE memberships m SET role = 'org_admin'
  FROM users u
 WHERE u.id = m.user_id AND m.role = 'org_owner' AND u.totp_enabled_at IS NULL
   AND (:'email' = '' OR lower(u.email) = lower(:'email'))
RETURNING u.email, 'org_owner -> org_admin' AS change;
SQL
		status ;;
	on)
		[ -n "${2:-}" ] || die "usage: $0 on <email>"
		sql -v email="$2" <<'SQL'
UPDATE memberships m SET role = 'org_owner'
  FROM users u
 WHERE u.id = m.user_id AND m.role = 'org_admin' AND lower(u.email) = lower(:'email')
RETURNING u.email, 'org_admin -> org_owner' AS change;
SQL
		status ;;
	*)
		die "usage: $0 status | off [email] | on <email>" ;;
esac
