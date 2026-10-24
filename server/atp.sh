#!/usr/bin/env bash
# ATP on your own machine — every server command in one script.
#
# Turns a personal computer (Linux, macOS, or Windows with Docker Desktop +
# Git Bash) into the ATP server: Postgres (TLS), migrate, api, worker,
# Prometheus, daily backups, and Caddy serving the dashboard over HTTPS —
# the stack of backend/deploy/compose.yml, driven from here.
#
#   ./server/atp.sh init [host]     first time: write the config (secrets generated)
#   ./server/atp.sh up              build the dashboard + images, start everything
#   ./server/atp.sh seed            first organization, project, owner account, CI token
#   ./server/atp.sh status          what runs, and is the api healthy
#   ./server/atp.sh doctor [--pilot] read-only deployment prerequisites
#
# Everything else:
#   down | restart | logs [service] | update
#   token <project-id> [name]       a new CI token (or use the dashboard: CI tokens)
#   admin <args…>                   the operator CLI (org/ws/project/user/role/token/audit…)
#   backup | backups | backup-copy <dir>
#   ca [file]                       export Caddy's root certificate for other devices
#   reporter                        re-pack the Playwright reporter served at /downloads/ (up does it too)
#   reset                           stop and DELETE ALL DATA (asks first)
#
# Requirements: docker (with compose v2; the dashboard is built in a Node
# container, so no Node needed here), git (for update), openssl or /dev/urandom.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEPLOY="$ROOT/backend/deploy"
ENV_FILE="$DEPLOY/.env"
# Git Bash rewrites /app/… arguments into Windows paths; docker must see them as they are.
export MSYS_NO_PATHCONV=1

# Paths handed to docker are native ones (C:/… on Windows): with path
# conversion off, docker would read a Git Bash /c/… or /tmp/… literally.
host_path() { if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else echo "$1"; fi; }
# URLCHECK=on in the config also runs the URL checker (compose profile).
dc() {
	local profile=()
	if [ -f "$ENV_FILE" ] && [ "$(grep -E '^URLCHECK=' "$ENV_FILE" | tail -1 | cut -d= -f2- | tr -d '\r')" = on ]; then
		profile=(--profile urlcheck)
	fi
	# ${a[@]+…}: an empty array under `set -u` is an error in macOS's bash 3.2.
	docker compose -f "$(host_path "$DEPLOY/compose.yml")" --env-file "$(host_path "$ENV_FILE")" ${profile[@]+"${profile[@]}"} "$@"
}
say() { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31mxx\033[0m %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is not installed ($2)"; }
need_env() { [ -f "$ENV_FILE" ] || die "no config yet — run: $0 init"; }
# The "ID"/"Plaintext" field of the admin CLI's JSON answer.
field() { grep -o "\"$1\": *\"[^\"]*\"" | head -1 | sed 's/.*: *"\(.*\)"/\1/'; }
env_get() { grep -E "^$1=" "$ENV_FILE" | tail -1 | cut -d= -f2-; }

secret() {
	if command -v openssl >/dev/null 2>&1; then openssl rand -hex "$1"
	else head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'; fi
}

# The address other machines on the network reach this one at — a guess to
# confirm, not a truth (VPNs and several network cards fool it).
lan_ip() {
	local ip=""
	if command -v ip >/dev/null 2>&1; then
		ip=$(ip route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p')
	elif command -v ipconfig >/dev/null 2>&1; then
		ip=$(ipconfig 2>/dev/null | tr -d '\r' | grep -iE "IPv4" | grep -oE "([0-9]{1,3}\.){3}[0-9]{1,3}" | grep -vE "^(127|169\.254)\." | head -1)
	fi
	echo "${ip:-localhost}"
}

wait_healthy() {
	local domain; domain=$(env_get DOMAIN)
	say "waiting for https://$domain/healthz"
	for _ in $(seq 1 60); do
		if curl -skf "https://$domain/healthz" >/dev/null 2>&1 || curl -skf https://localhost/healthz >/dev/null 2>&1; then
			say "api is up"; return 0
		fi
		sleep 3
	done
	dc ps; die "the api did not answer in 3 minutes — look at: $0 logs api"
}

cmd_init() {
	need docker "https://docs.docker.com/get-docker/"
	if [ -f "$ENV_FILE" ]; then
		warn "$ENV_FILE exists — left as it is (delete it to start over; the secrets in it protect your data)"
		return 0
	fi
	local host="${1:-}"
	if [ -z "$host" ]; then
		local guess; guess=$(lan_ip)
		read -r -p "Address other devices will use to open the dashboard [$guess]: " host || true
		host="${host:-$guess}"
	fi
	local store="memory" endpoint="https://$host/_objstore" bucket="" key="" skey="" appenv="dev"
	echo
	echo "Artifacts (screenshots, videos, traces) need a bucket to survive a restart."
	echo "  1) none — keep them in memory, lost when the api restarts (fine to try things out)"
	echo "  2) an S3 / R2 / MinIO bucket I have"
	read -r -p "Choose [1]: " choice || true
	if [ "${choice:-1}" = "2" ]; then
		store="s3"; appenv="staging"
		read -r -p "  Endpoint URL (e.g. https://<account>.r2.cloudflarestorage.com): " endpoint
		read -r -p "  Bucket: " bucket
		read -r -p "  Access key: " key
		read -r -s -p "  Secret key: " skey; echo
	fi
	cp "$DEPLOY/.env.example" "$ENV_FILE"
	set_kv() { # key value — replace or append, value taken literally
		local k="$1" v="$2" tmp; tmp=$(mktemp)
		awk -v k="$k" -v v="$v" 'BEGIN{done=0} $0 ~ "^"k"=" {print k"="v; done=1; next} {print} END{if(!done) print k"="v}' "$ENV_FILE" >"$tmp"
		mv "$tmp" "$ENV_FILE"
	}
	set_kv POSTGRES_PASSWORD "$(secret 24)"
	set_kv TOKEN_PEPPER "$(secret 32)"
	set_kv DOMAIN "$host"
	# The server itself may open the dashboard as localhost too.
	local origins="https://$host"
	[ "$host" = localhost ] || origins="$origins,https://localhost"
	set_kv DASHBOARD_ORIGINS "$origins"
	set_kv PUBLIC_URL "https://$host"
	set_kv APP_ENV "$appenv"
	set_kv OBJSTORE "$store"
	set_kv OBJSTORE_ENDPOINT "$endpoint"
	set_kv OBJSTORE_BUCKET "$bucket"
	set_kv OBJSTORE_ACCESS_KEY "$key"
	set_kv OBJSTORE_SECRET_KEY "$skey"
	chmod 600 "$ENV_FILE" 2>/dev/null || true
	say "wrote $ENV_FILE (secrets generated; back this file up — without TOKEN_PEPPER every token and MFA enrolment is lost)"
	echo "    Sign-in with Google/GitHub and email (invitations, forgot password) are off;"
	echo "    turn them on in that file (OAUTH_*, MAIL=smtp) and run: $0 restart"
	if [ "$host" != localhost ]; then
		echo "    Other devices reach it at https://$host — allow TCP 443 (and 80) in this machine's firewall;"
		echo "    they must also trust its certificate: after up, run $0 ca"
	fi
	echo "Next: $0 up"
}

# The dashboard is built in a throwaway Node 22 container (the Node CI uses),
# from a copy of frontend/ without its node_modules: the working copy's
# node_modules is never touched — on a machine that is also a developer's,
# `npm ci` there would wipe the modules a running dev server or editor holds
# (and npm 11 would rewrite package-lock.json). Only dist/ comes back.
build_ui() {
	say "building the dashboard in a Node 22 container (frontend/dist)"
	mkdir -p "$ROOT/frontend/dist"
	docker run --rm \
		-v "$(host_path "$ROOT/frontend"):/src:ro" \
		-v "$(host_path "$ROOT/frontend/dist"):/out" \
		-v atp-npm-cache:/root/.npm \
		node:22-alpine sh -ec '
			mkdir /build
			cd /src && tar --exclude=./node_modules --exclude=./dist --exclude=./test-results -cf - . | tar -xf - -C /build
			cd /build && npm ci --no-audit --no-fund --loglevel=error && npm run build
			rm -rf /out/* && cp -r dist/. /out/'
}

# The Playwright reporter as an npm tarball that Caddy serves at /downloads/
# (QĐ-14): a test project installs it from this server, without a copy of
# the ATP repository. Packed like the dashboard is built, in a throwaway
# Node container from a copy of reporter/playwright; only the tarball and
# index.json come back (reporter/playwright/scripts/pack-for-server.mjs
# explains the file names and why old tarballs stay).
build_reporter() {
	say "packing the Playwright reporter (backend/deploy/downloads)"
	mkdir -p "$DEPLOY/downloads"
	docker run --rm \
		-v "$(host_path "$ROOT/reporter/playwright"):/src:ro" \
		-v "$(host_path "$DEPLOY/downloads"):/out" \
		-v atp-npm-cache:/root/.npm \
		node:22-alpine sh -ec '
			mkdir /build
			cd /src && tar --exclude=./node_modules --exclude=./dist --exclude=./test-results -cf - . | tar -xf - -C /build
			cd /build && npm ci --no-audit --no-fund --loglevel=error && npm run build
			node scripts/pack-for-server.mjs /out'
}

cmd_up() {
	need_env; need docker "https://docs.docker.com/get-docker/"
	build_ui
	build_reporter
	say "building images and starting the stack"
	dc up -d --build
	wait_healthy
	cmd_status
}

cmd_status() {
	need_env
	dc ps --format 'table {{.Service}}\t{{.State}}\t{{.Status}}'
	local domain; domain=$(env_get DOMAIN)
	if curl -skf "https://$domain/healthz" >/dev/null 2>&1; then
		say "dashboard: https://$domain  (reporter: ATP_URL=https://$domain)"
	else
		warn "https://$domain/healthz does not answer from here"
	fi
}

cmd_doctor() {
	local pilot=false failures=0 daemon=false value key
	case "${1:-}" in
		"") [ "$#" -eq 0 ] || die "usage: $0 doctor [--pilot]" ;;
		--pilot) [ "$#" -eq 1 ] || die "usage: $0 doctor [--pilot]"; pilot=true ;;
		*) die "usage: $0 doctor [--pilot]" ;;
	esac
	say "read-only deployment checks (no containers started or config changed)"
	if ! command -v docker >/dev/null 2>&1; then
		warn "FAIL: docker is not installed"; failures=$((failures + 1))
	else
		if docker compose version >/dev/null 2>&1; then say "PASS: Docker Compose available"
		else warn "FAIL: Docker Compose v2 is unavailable"; failures=$((failures + 1)); fi
		if value=$(docker info --format '{{.OSType}}' 2>/dev/null); then
			value=${value//$'\r'/}
			if [ "$value" = linux ]; then daemon=true; say "PASS: Linux Docker engine reachable"
			else warn "FAIL: switch Docker Desktop to Linux containers"; failures=$((failures + 1)); fi
		else warn "FAIL: Docker engine unreachable; start Docker Desktop on the deployment host"; failures=$((failures + 1)); fi
	fi
	if ! command -v curl >/dev/null 2>&1; then
		warn "FAIL: curl is required for deployment health checks"; failures=$((failures + 1))
	fi
	if [ ! -f "$ENV_FILE" ]; then
		warn "FAIL: deployment config missing; run $0 init on the deployment host"
		failures=$((failures + 1))
	else
		for key in POSTGRES_PASSWORD TOKEN_PEPPER DOMAIN DASHBOARD_ORIGINS; do
			value=$(env_get "$key" || true)
			case "$value" in
				""|*change-me*|*'<account>'*) warn "FAIL: $key is missing or still a placeholder"; failures=$((failures + 1)) ;;
			esac
		done
		value=$(env_get TOKEN_PEPPER || true)
		if [ "${#value}" -lt 32 ]; then warn "FAIL: TOKEN_PEPPER must contain at least 32 bytes"; failures=$((failures + 1)); fi
		if [ "$(env_get OBJSTORE || true)" = s3 ]; then
			for key in OBJSTORE_ENDPOINT OBJSTORE_BUCKET OBJSTORE_ACCESS_KEY OBJSTORE_SECRET_KEY; do
				value=$(env_get "$key" || true)
				case "$value" in
					""|*change-me*|*'<'*|*'>'*) warn "FAIL: $key is missing or still a placeholder"; failures=$((failures + 1)) ;;
				esac
			done
			say "S3 storage configured; bucket access and restart persistence still require a real test"
		elif [ "$(env_get OBJSTORE || true)" = memory ]; then
			if $pilot; then warn "FAIL: pilot requires durable S3-compatible artifact storage"; failures=$((failures + 1))
			else warn "Memory artifacts disappear on API restart; this config is only for trying the app"; fi
		else warn "FAIL: OBJSTORE must be memory or s3"; failures=$((failures + 1)); fi
		if $pilot; then
			case "$(env_get APP_ENV || true)" in
				staging|prod) ;;
				*) warn "FAIL: pilot requires APP_ENV=staging or prod"; failures=$((failures + 1)) ;;
			esac
			if [ "$(env_get COOKIE_SECURE || true)" != true ]; then warn "FAIL: pilot requires COOKIE_SECURE=true"; failures=$((failures + 1)); fi
		fi
		if $daemon; then
			# Never print compose's resolved config: it contains credentials.
			if dc config --quiet >/dev/null 2>&1; then say "PASS: Compose configuration validates"
			else warn "FAIL: Compose configuration invalid; review deploy/.env and compose.yml locally"; failures=$((failures + 1)); fi
		fi
	fi
	warn "Manual acceptance still required: trusted HTTPS/LAN, bucket access, restart persistence, off-host DB/artifact backup and restore"
	if [ "$failures" -ne 0 ]; then warn "$failures prerequisite check(s) failed"; return 1; fi
	say "prerequisites passed; this is not pilot or production acceptance"
}

cmd_seed() {
	need_env
	local org ws project email password
	read -r -p "Organization name [My Company]: " org; org="${org:-My Company}"
	read -r -p "Workspace name [Engineering]: " ws; ws="${ws:-Engineering}"
	read -r -p "First project name [web-app]: " project; project="${project:-web-app}"
	read -r -p "Your email: " email; [ -n "$email" ] || die "an email is needed"
	read -r -s -p "Your password (12+ characters): " password; echo
	[ "${#password}" -ge 12 ] || die "the password needs at least 12 characters"
	local slug; slug=$(echo "$org" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]\{1,\}/-/g; s/^-//; s/-$//')
	local A=(run --rm -T admin)
	say "organization $org"
	local org_id; org_id=$(dc "${A[@]}" org create -name "$org" -slug "${slug:-org}" | field ID) || true
	[ -n "$org_id" ] || die "could not create the organization (does the slug '$slug' exist already?)"
	local ws_id; ws_id=$(dc "${A[@]}" ws create -org "$org_id" -name "$ws" | field ID)
	local project_id; project_id=$(dc "${A[@]}" project create -ws "$ws_id" -name "$project" | field ID)
	say "account $email (organization owner)"
	local user_id; user_id=$(dc "${A[@]}" user create -email "$email" -password "$password" -verified | field ID)
	[ -n "$user_id" ] || die "could not create the account (is the email taken?)"
	dc "${A[@]}" role grant -user "$user_id" -level organization -scope "$org_id" -role org_owner >/dev/null
	local token; token=$(dc "${A[@]}" token issue -project "$project_id" -name "first-ci" | field Plaintext)
	local domain; domain=$(env_get DOMAIN)
	echo
	say "done"
	cat <<EOF
  Dashboard   https://$domain   (sign in as $email)
              Organization owners must turn on two-factor authentication at first sign-in.
  Project     $project  ($project_id)
  CI token    $token
              Shown once. Put it in your CI as the secret ATP_TOKEN, with ATP_URL=https://$domain
              More tokens: dashboard → project → CI tokens.
EOF
}

cmd_token() {
	need_env
	local project="${1:?usage: $0 token <project-id> [name]}" name="${2:-ci}"
	dc run --rm -T admin token issue -project "$project" -name "$name"
}

cmd_backup() {
	need_env
	say "backing up the database now"
	dc exec -T backup /backup/backup.sh
}

cmd_backups() {
	need_env
	dc exec -T backup ls -lh /backups
}

cmd_backup_copy() {
	need_env
	local dir="${1:?usage: $0 backup-copy <dir>}"
	mkdir -p "$dir"
	# A backup beside the database is not a backup: copy the dumps off this disk.
	dc cp backup:/backups/. "$(host_path "$dir")"
	say "dumps copied to $dir — keep them on another disk or machine"
}

cmd_ca() {
	need_env
	local out="${1:-$ROOT/server/atp-root-ca.crt}"
	dc cp caddy:/data/caddy/pki/authorities/local/root.crt "$(host_path "$out")"
	say "Caddy's root certificate: $out"
	cat <<EOF
  Install it as a trusted root on each device that opens the dashboard or runs a reporter,
  or browsers warn and CI reporters refuse the connection:
    Windows  certutil -addstore -f Root "$out"   (admin prompt)
    macOS    sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain "$out"
    Linux    sudo cp "$out" /usr/local/share/ca-certificates/atp.crt && sudo update-ca-certificates
    Node CI  NODE_EXTRA_CA_CERTS=/path/to/atp-root-ca.crt
  (A real DNS name in DOMAIN gets a public Let's Encrypt certificate instead — nothing to install.)
EOF
}

cmd_update() {
	need_env; need git "https://git-scm.com"
	say "pulling the latest code"
	git -C "$ROOT" pull --ff-only
	cmd_backup || warn "backup before update failed — continuing"
	cmd_up
}

cmd_reset() {
	need_env
	warn "This stops ATP and DELETES every run, result, account and backup volume on this machine."
	read -r -p "Type DELETE to continue: " ok
	[ "$ok" = "DELETE" ] || die "nothing deleted"
	dc down -v
	say "stopped and wiped; $ENV_FILE is kept (delete it too to change the secrets)"
}

case "${1:-help}" in
	init) shift; cmd_init "$@" ;;
	up | start) cmd_up ;;
	down | stop) need_env; dc down ;;
	restart) need_env; dc up -d; wait_healthy ;;
	status | ps) cmd_status ;;
	doctor) shift; cmd_doctor "$@" ;;
	logs) need_env; shift; dc logs -f --tail=200 "$@" ;;
	seed) cmd_seed ;;
	token) shift; cmd_token "$@" ;;
	admin) need_env; shift; dc run --rm -T admin "$@" ;;
	backup) cmd_backup ;;
	backups) cmd_backups ;;
	backup-copy) shift; cmd_backup_copy "$@" ;;
	ca) shift; cmd_ca "$@" ;;
	update) cmd_update ;;
	reporter) need docker "https://docs.docker.com/get-docker/"; build_reporter ;;
	reset) cmd_reset ;;
	help | -h | --help) sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//' ;;
	*) die "unknown command '$1' — see: $0 help" ;;
esac
