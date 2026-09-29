#!/usr/bin/env bash
# ATP on your own machine — every server command in one script.
#
# Turns a personal computer (Linux, macOS, or Windows with Docker Desktop +
# Git Bash) into the ATP server: Postgres (TLS), migrate, api, worker,
# Prometheus, daily backups, and Caddy serving the dashboard over HTTPS —
# the stack of backend/deploy/compose.yml, driven from here.
#
#   ./server/atp.sh init [host]     first time: write the config (secrets generated;
#                                   HTTPS_PORT=8443 HTTP_PORT=8080 … init when 443/80 are taken)
#   ./server/atp.sh up              build the dashboard + images, start everything
#   ./server/atp.sh seed            first organization, project, owner account, CI token
#   ./server/atp.sh status          what runs, and is the api healthy
#
# Everything else:
#   down | restart | logs [service] | update
#   token <project-id> [name]       a new CI token (or use the dashboard: CI tokens)
#   admin <args…>                   the operator CLI (org/ws/project/user/role/token/audit…)
#   backup | backups | backup-copy <dir>
#   ca [file]                       export Caddy's root certificate for other devices
#   firewall                        open HTTPS_PORT (tcp+udp) and HTTP_PORT in this machine's firewall
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
dc() { docker compose -f "$(host_path "$DEPLOY/compose.yml")" --env-file "$(host_path "$ENV_FILE")" "$@"; }
say() { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31mxx\033[0m %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is not installed ($2)"; }
need_env() { [ -f "$ENV_FILE" ] || die "no config yet — run: $0 init"; }
# A field ("ProjectID", "Plaintext"…) of the admin CLI's JSON answer.
field() { grep -o "\"$1\": *\"[^\"]*\"" | head -1 | sed 's/.*: *"\(.*\)"/\1/'; }
env_get() { { grep -E "^$1=" "$ENV_FILE" || true; } | tail -1 | cut -d= -f2-; }
# The dashboard's address as browsers see it: the port only when Caddy is not on 443.
https_port() { local p; p=$(env_get HTTPS_PORT); echo "${p:-443}"; }
url_for() { if [ "$2" = 443 ]; then echo "https://$1"; else echo "https://$1:$2"; fi; }
base_url() { url_for "$(env_get DOMAIN)" "$(https_port)"; }
# Asks this machine's Caddy for /healthz under the name it serves ($DOMAIN
# only — it has no certificate for localhost), without depending on the
# machine reaching its own public name.
healthy() {
	local domain port; domain=$(env_get DOMAIN); port=$(https_port)
	curl -skf --resolve "$domain:$port:127.0.0.1" "$(url_for "$domain" "$port")/healthz" >/dev/null 2>&1 ||
		curl -skf "$(url_for "$domain" "$port")/healthz" >/dev/null 2>&1
}

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
	say "waiting for $(base_url)/healthz"
	for _ in $(seq 1 60); do
		if healthy; then
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
	# Another server owns 443? Run: HTTPS_PORT=8443 HTTP_PORT=8080 $0 init
	local port="${HTTPS_PORT:-443}" hport="${HTTP_PORT:-80}"
	case "$port$hport" in *[!0-9]*) die "HTTPS_PORT and HTTP_PORT must be numbers" ;; esac
	local url; url=$(url_for "$host" "$port")
	local store="memory" endpoint="$url/_objstore" bucket="" key="" skey="" appenv="dev"
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
	set_kv HTTPS_PORT "$port"
	set_kv HTTP_PORT "$hport"
	# Exactly the origin browsers send — Caddy serves $DOMAIN only, so the
	# server itself opens the dashboard at this address too, not localhost.
	set_kv DASHBOARD_ORIGINS "$url"
	set_kv PUBLIC_URL "$url"
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
		echo "    Other devices reach it at $url — open the ports with: $0 firewall"
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
		-e OWNER="$(id -u):$(id -g)" \
		node:22-alpine sh -ec '
			mkdir /build
			cd /src && tar --exclude=./node_modules --exclude=./dist --exclude=./test-results -cf - . | tar -xf - -C /build
			cd /build && npm ci --no-audit --no-fund --loglevel=error && npm run build
			rm -rf /out/* && cp -r dist/. /out/
			# The container is root: hand dist/ back to whoever ran this, or a
			# later npm run build / rm -rf dist on this machine is refused.
			chown -R "$OWNER" /out 2>/dev/null || true'
}

cmd_up() {
	need_env; need docker "https://docs.docker.com/get-docker/"
	case ",$(env_get DASHBOARD_ORIGINS)," in
		*",$(base_url),"*) ;;
		*) warn "DASHBOARD_ORIGINS in $ENV_FILE does not list $(base_url) — signed-in changes from that address will be refused" ;;
	esac
	build_ui
	say "building images and starting the stack"
	dc up -d --build
	wait_healthy
	cmd_status
}

cmd_status() {
	need_env
	dc ps --format 'table {{.Service}}\t{{.State}}\t{{.Status}}'
	local url; url=$(base_url)
	if healthy; then
		say "dashboard: $url  (reporter: ATP_URL=$url)"
	else
		warn "$url/healthz does not answer from here"
	fi
}

cmd_seed() {
	need_env
	local org ws project email password
	read -r -p "Organization name [My Company]: " org; org="${org:-My Company}"
	read -r -p "Workspace name [Engineering]: " ws; ws="${ws:-Engineering}"
	read -r -p "First project name [web-app]: " project; project="${project:-web-app}"
	read -r -p "Your email: " email
	case "$email" in ?*@?*) ;; *) die "that is not an email address" ;; esac
	read -r -s -p "Your password (12–256 characters): " password; echo
	[ "${#password}" -ge 12 ] || die "the password needs at least 12 characters"
	[ "$(printf %s "$password" | wc -c)" -le 256 ] || die "the password can be at most 256 bytes"
	local slug; slug=$(echo "$org" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]\{1,\}/-/g; s/^-//; s/-$//')
	say "organization $org, owner $email"
	# One admin call, one transaction: if anything is refused nothing is kept,
	# so seed can be run again. The password goes on stdin (printf is a shell
	# builtin), never in an argument that ps would show.
	local out errf; errf=$(mktemp)
	if ! out=$(printf '%s\n' "$password" | dc run --rm -T admin org bootstrap -name "$org" -slug "${slug:-org}" \
		-ws "$ws" -project "$project" -email "$email" -token first-ci 2>"$errf"); then
		local why; why=$(grep '^admin:' "$errf" | tail -1)
		[ -n "$why" ] || why=$(tail -5 "$errf")
		rm -f "$errf"
		die "nothing was created: ${why:-the admin command failed} (a taken slug '$slug' or email? run seed again with other values)"
	fi
	rm -f "$errf"
	local project_id token url
	project_id=$(echo "$out" | field ProjectID)
	token=$(echo "$out" | field Plaintext)
	url=$(base_url)
	echo
	say "done"
	cat <<EOF
  Dashboard   $url   (sign in as $email)
              Organization owners must turn on two-factor authentication at first sign-in.
  Project     $project  ($project_id)
  CI token    $token
              Shown once. Put it in your CI as the secret ATP_TOKEN, with ATP_URL=$url
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

# Opens the ports Caddy publishes (HTTPS_PORT tcp + udp for HTTP/3, HTTP_PORT
# for the http→https redirect and Let's Encrypt) in whichever firewall this
# machine runs. Rules are named/added idempotently, so it can run again.
# It never switches a firewall on: enabling ufw from a script can lock out
# the SSH session running it. A router, cloud security group or VPN in
# front of this machine is not reached from here.
cmd_firewall() {
	need_env
	local https http; https=$(https_port); http=$(env_get HTTP_PORT); http="${http:-80}"
	case "$https$http" in *[!0-9]*) die "HTTPS_PORT/HTTP_PORT in $ENV_FILE must be numbers" ;; esac
	local sudo=""
	case "$(uname -s)" in
	MINGW* | MSYS* | CYGWIN*)
		net session >/dev/null 2>&1 || die "run Git Bash as administrator (right click → Run as administrator), then: $0 firewall"
		local rule name proto port
		for rule in "ATP HTTPS TCP:TCP:$https" "ATP HTTPS UDP:UDP:$https" "ATP HTTP:TCP:$http"; do
			IFS=: read -r name proto port <<<"$rule"
			netsh advfirewall firewall delete rule name="$name" >/dev/null 2>&1 || true
			netsh advfirewall firewall add rule name="$name" dir=in action=allow protocol="$proto" localport="$port" >/dev/null ||
				die "netsh refused the rule '$name'"
			say "Windows firewall: $proto $port allowed ($name)"
		done
		;;
	Linux)
		if [ "$(id -u)" != 0 ]; then need sudo "or run this as root"; sudo=sudo; fi
		if command -v ufw >/dev/null 2>&1 && $sudo ufw status 2>/dev/null | grep -q "Status: active"; then
			$sudo ufw allow "$https/tcp" >/dev/null
			$sudo ufw allow "$https/udp" >/dev/null
			$sudo ufw allow "$http/tcp" >/dev/null
			say "ufw: $https/tcp, $https/udp, $http/tcp allowed"
		elif command -v firewall-cmd >/dev/null 2>&1 && $sudo firewall-cmd --state >/dev/null 2>&1; then
			$sudo firewall-cmd --permanent --add-port="$https/tcp" --add-port="$https/udp" --add-port="$http/tcp" >/dev/null
			$sudo firewall-cmd --reload >/dev/null
			say "firewalld: $https/tcp, $https/udp, $http/tcp allowed (permanent)"
		else
			say "no active ufw or firewalld here — nothing on this machine blocks $https/$http"
			echo "    Still unreachable? Check iptables/nftables rules, the router, or the cloud security group."
		fi
		;;
	Darwin)
		say "macOS filters by app, not by port: when asked, allow Docker to accept incoming connections"
		echo "    (System Settings → Network → Firewall → Options → Docker: Allow incoming connections)"
		;;
	*) die "unknown system $(uname -s) — open TCP $https, UDP $https and TCP $http in its firewall yourself" ;;
	esac
	echo "    Other devices: $(base_url)"
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
	logs) need_env; shift; dc logs -f --tail=200 "$@" ;;
	seed) cmd_seed ;;
	token) shift; cmd_token "$@" ;;
	admin) need_env; shift; dc run --rm -T admin "$@" ;;
	backup) cmd_backup ;;
	backups) cmd_backups ;;
	backup-copy) shift; cmd_backup_copy "$@" ;;
	ca) shift; cmd_ca "$@" ;;
	firewall) cmd_firewall ;;
	update) cmd_update ;;
	reset) cmd_reset ;;
	help | -h | --help) sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//' ;;
	*) die "unknown command '$1' — see: $0 help" ;;
esac
