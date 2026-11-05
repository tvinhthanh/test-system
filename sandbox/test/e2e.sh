#!/usr/bin/env bash
# sandboxd end to end on a real Docker host (QĐ-19 tier 4) — what unit tests
# with a fake Docker cannot prove: the job network has no way out but the
# forwarder, the container is unprivileged and read-only, Playwright (two
# engines) and k6 run and report, a run past its limit is stopped, and
# nothing is left behind. CI runs it (job "sandbox"); by hand:
#
#   bash sandbox/test/e2e.sh        (from the repository root; needs docker, jq, curl)
#
# The fixture site sits on a private Docker network, so sandboxd runs with
# SANDBOX_ALLOW_PRIVATE=on here; the refusals tested are the ones that hold
# either way (no DNS, no route, the api closed to jobs).
set -euo pipefail
cd "$(dirname "$0")/../.."
ROOT=$PWD
TOKEN=$(openssl rand -hex 32)
NET=atp-sbx-e2e
fail() { echo "FAIL: $*" >&2; exit 1; }
cleanup() {
	docker logs sandboxd-e2e >"${E2E_LOGS:-/tmp}/sandboxd-e2e.log" 2>&1 || true
	docker rm -f sandboxd-e2e site-e2e >/dev/null 2>&1 || true
	docker network rm "$NET" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "== images"
docker build -q -t atp/backend:sbx-e2e -f backend/deploy/Dockerfile backend
docker build -q -t atp/sandbox-playwright:local sandbox/playwright
docker pull -q grafana/k6:1.0.0

echo "== fixture site and sandboxd"
docker network create "$NET" >/dev/null
docker run -d --name site-e2e --network "$NET" --network-alias site node:22-bookworm-slim \
	node -e "require('http').createServer((q, s) => { s.setHeader('content-type', 'text/html'); s.end('<title>Sandbox fixture</title><h1>hello sandbox</h1>'); }).listen(80)" >/dev/null
docker run -d --name sandboxd-e2e --network "$NET" -p 127.0.0.1:8091:8091 \
	-v /var/run/docker.sock:/var/run/docker.sock --group-add "$(stat -c %g /var/run/docker.sock)" \
	--sysctl net.ipv4.ip_unprivileged_port_start=0 --read-only --cap-drop ALL --security-opt no-new-privileges:true \
	-e SANDBOX_TOKEN="$TOKEN" -e SANDBOX_ALLOW_PRIVATE=on -e SANDBOX_MEMORY_MB=1536 -e SANDBOX_CONCURRENCY=1 \
	--entrypoint /app/sandboxd atp/backend:sbx-e2e >/dev/null
for _ in $(seq 1 30); do curl -sf http://127.0.0.1:8091/healthz >/dev/null && break; sleep 1; done
curl -sf http://127.0.0.1:8091/healthz >/dev/null || fail "sandboxd is not healthy"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:8091/run -d '{}')
[ "$code" = 401 ] || fail "no token: $code"

# run <job.json> → the answer on stdout
run() { curl -sS --max-time 900 -X POST http://127.0.0.1:8091/run -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' --data-binary "@$1"; }
job() { # kind dir extra-jq
	local id; id=$(cat /proc/sys/kernel/random/uuid)
	tar -czf - -C "$2" . | base64 -w0 >/tmp/archive.b64
	jq -n --arg id "$id" --arg kind "$1" --rawfile a /tmp/archive.b64 "{id: \$id, kind: \$kind, base: \"http://site\", archive: (\$a | rtrimstr(\"\\n\")), timeoutSeconds: 600} + $3" >/tmp/job.json
}
left() {
	[ -z "$(docker ps -aq --filter label=atp.sandbox=1)" ] || fail "a job container was left behind"
	[ -z "$(docker network ls -q --filter label=atp.sandbox=1)" ] || fail "a job network was left behind"
}

echo "== playwright: chromium + firefox"
job playwright sandbox/test/pw '{grep: "sandbox"}'
run /tmp/job.json >/tmp/pw.json
jq -r '.log' /tmp/pw.json
[ "$(jq -r '.error // ""' /tmp/pw.json)" = "" ] || fail "playwright: $(jq -r .error /tmp/pw.json)"
[ "$(jq -r '.exitCode' /tmp/pw.json)" = 1 ] || fail "playwright exit code: $(jq -r .exitCode /tmp/pw.json) (one test fails on purpose)"
jq -r '.files["junit.xml"]' /tmp/pw.json | base64 -d >/tmp/junit.xml
cases=$(grep -o '<testcase ' /tmp/junit.xml | wc -l)
failed=$(grep -o '<failure' /tmp/junit.xml | wc -l)
[ "$cases" = 8 ] || fail "want 8 testcases (4 tests × 2 engines), got $cases"
[ "$failed" = 2 ] || { cat /tmp/junit.xml; fail "want only the on-purpose failure on each engine, got $failed failures"; }
grep -q 'this test fails on purpose' /tmp/junit.xml || fail "the on-purpose failure is not the one that failed"
left

echo "== k6: the run's vus replace the script's 500"
job k6 sandbox/test/k6 '{entry: "load.js", vus: 2, seconds: 5}'
run /tmp/job.json >/tmp/k6.json
jq -r '.log' /tmp/k6.json | tail -40
[ "$(jq -r '.error // ""' /tmp/k6.json)" = "" ] || fail "k6: $(jq -r .error /tmp/k6.json)"
[ "$(jq -r '.exitCode' /tmp/k6.json)" = 0 ] || fail "k6 exit code: $(jq -r .exitCode /tmp/k6.json)"
jq -r '.files["summary.json"]' /tmp/k6.json | base64 -d >/tmp/summary.json
jq -e '.metrics.http_reqs.count > 0' /tmp/summary.json >/dev/null || fail "k6 sent no request"
jq -r '.log' /tmp/k6.json | grep -Eq 'vus_max[^0-9]*2([^0-9]|$)|max=2' || fail "k6 did not run with the run's 2 VUs"
left

echo "== a run past its limit is stopped"
job playwright sandbox/test/pw '{grep: "hangs", project: "chromium", timeoutSeconds: 45}'
start=$(date +%s)
run /tmp/job.json >/tmp/slow.json
took=$(( $(date +%s) - start ))
[ "$(jq -r '.timedOut' /tmp/slow.json)" = true ] || fail "not stopped: $(jq -c '{exitCode, timedOut, error}' /tmp/slow.json)"
[ "$took" -lt 120 ] || fail "stopping took ${took}s"
left

echo "== refused before anything runs"
job playwright sandbox/test/pw '{base: "http://169.254.169.254"}'
[ -n "$(run /tmp/job.json | jq -r '.error')" ] || fail "an IP base was accepted"
job k6 sandbox/test/k6 '{entry: "load.js", vus: 5000, seconds: 5}'
[ -n "$(run /tmp/job.json | jq -r '.error')" ] || fail "5000 VUs were accepted"
left
echo "OK: sandboxd end to end"
