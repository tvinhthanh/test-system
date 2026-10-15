#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
temp_root="$(cd "${TMPDIR:-/tmp}" && pwd -P)"
sandbox=$(mktemp -d "$temp_root/atp-doctor.XXXXXXXX")
sandbox=$(cd "$sandbox" && pwd -P)
case "$sandbox" in "$temp_root"/atp-doctor.*) ;; *) exit 1 ;; esac
trap 'rm -rf -- "$sandbox"' EXIT
mkdir -p "$sandbox/repo with spaces/server" "$sandbox/repo with spaces/backend/deploy" "$sandbox/bin"
cp "$ROOT/server/atp.sh" "$sandbox/repo with spaces/server/atp.sh"
cp "$ROOT/server/tests/fixtures/docker" "$sandbox/bin/docker"
chmod +x "$sandbox/bin/docker"
export PATH="$sandbox/bin:$PATH"
deploy="$sandbox/repo with spaces/backend/deploy"
script="$sandbox/repo with spaces/server/atp.sh"

check() {
  local expected="$1" label="$2" status=0
  shift 2
  bash "$script" doctor "$@" >"$sandbox/output" 2>&1 || status=$?
  [ "$status" -eq "$expected" ] || { printf 'FAIL: %s\n' "$label"; exit 1; }
  if grep -qE 'test-only-(access|secret|pepper|postgres)|Unexpected Docker command' "$sandbox/output"; then
    printf 'FAIL: secret leak or unexpected mutation: %s\n' "$label"; exit 1
  fi
  printf 'PASS: %s\n' "$label"
}
cp "$ROOT/server/tests/fixtures/doctor.env" "$deploy/.env"
before=$(cksum "$deploy/.env")
check 0 'configured pilot prerequisites' --pilot
[ "$before" = "$(cksum "$deploy/.env")" ]
check 1 'invalid arguments rejected' --unknown
DOCTOR_NO_ENGINE=1 check 1 'stopped engine detected'
DOCTOR_ENGINE=windows check 1 'Windows container mode rejected'
DOCTOR_NO_COMPOSE=1 check 1 'missing Compose detected'
DOCTOR_BAD_CONFIG=1 check 1 'bad Compose config detected'
printf '\nOBJSTORE=memory\nAPP_ENV=dev\n' >>"$deploy/.env"
check 0 'memory demo allowed with warning'
grep -q 'disappear on API restart' "$sandbox/output"
check 1 'memory demo rejected for pilot' --pilot
cp "$ROOT/server/tests/fixtures/doctor.env" "$deploy/.env"
printf '\nCOOKIE_SECURE=false\n' >>"$deploy/.env"
check 1 'insecure pilot cookies rejected' --pilot
cp "$ROOT/server/tests/fixtures/doctor.env" "$deploy/.env"
printf '\nOBJSTORE_SECRET_KEY=\nTOKEN_PEPPER=change-me\n' >>"$deploy/.env"
check 1 'missing credentials and placeholder detected' --pilot
rm -- "$deploy/.env"
check 1 'missing deployment config detected'
