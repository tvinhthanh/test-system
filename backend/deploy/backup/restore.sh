#!/usr/bin/env bash
# Restore a backup.sh dump into a NEW database (SPEC §11.5: RTO 4 h), then
# prove it before pointing anything at it. Never restores over the live
# database: the live one may still hold rows the backup does not, and the
# decision to abandon them is a human's, made after looking at both.
#
#   restore.sh <dump-file> <target-db-url>
#
#   target-db-url   postgres://…/atp_restored — the database must NOT exist;
#                   the script creates it with the source's encoding/locale
#                   (read from the dump's original database is not possible,
#                   so pass SOURCE_DB_URL to copy them, else the server default)
#   SOURCE_DB_URL   optional: where to read encoding/collation from
#   PG_BIN          directory of pg_restore/psql (default: PATH)
#
# Afterwards:
#   1. DATABASE_URL=<target> go run ./cmd/migrate   → must print nothing to apply
#   2. start one api against it, log in, open the newest run
#   3. switch DATABASE_URL of api + worker; the worker finishes jobs that
#      were pending at backup time on its own
# test/restore/ automates exactly this sequence against a seeded world.
set -euo pipefail

dump="${1:?usage: restore.sh <dump-file> <target-db-url>}"
target="${2:?usage: restore.sh <dump-file> <target-db-url>}"
PG_RESTORE="${PG_BIN:+$PG_BIN/}pg_restore"
PSQL="${PG_BIN:+$PG_BIN/}psql"

[ -f "$dump" ] || { echo "restore: $dump not found" >&2; exit 1; }
"$PG_RESTORE" --list --file="$dump.toc" "$dump" && rm -f "$dump.toc"

# Split the target URL into "server" (…/postgres) and database name.
dbname="${target##*/}"; dbname="${dbname%%\?*}"
query=""; case "$target" in *\?*) query="?${target#*\?}";; esac
server="${target%/*}/postgres$query"

encoding="UTF8"; collate=""; ctype=""
if [ -n "${SOURCE_DB_URL:-}" ]; then
  read -r encoding collate ctype < <("$PSQL" "$SOURCE_DB_URL" -Atc \
    "SELECT pg_encoding_to_char(encoding), datcollate, datctype FROM pg_database WHERE datname = current_database()" | tr -d '\r' | tr '|' ' ')
fi
locale_clause=""
[ -n "$collate" ] && locale_clause="LC_COLLATE '$collate' LC_CTYPE '$ctype'"

start=$(date +%s)
"$PSQL" "$server" -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"$dbname\" TEMPLATE template0 ENCODING '$encoding' $locale_clause"
# Extensions are database-level and created by the dump for a full-database
# backup; for a schema-only dump (the drill's shape) they must exist first.
"$PSQL" "$target" -v ON_ERROR_STOP=1 -c "CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public" -c "CREATE EXTENSION IF NOT EXISTS btree_gin WITH SCHEMA public"
"$PG_RESTORE" --no-owner --no-privileges --exit-on-error --dbname="$target" "$dump"
took=$(( $(date +%s) - start ))

echo "restore: $dump → $dbname in ${took}s"
"$PSQL" "$target" -Atc "SELECT 'runs', count(*) FROM runs UNION ALL SELECT 'test_results', count(*) FROM test_results UNION ALL SELECT 'jobs pending', count(*) FROM jobs WHERE status = 'pending' UNION ALL SELECT 'schema_migrations', count(*) FROM schema_migrations"
echo "next: DATABASE_URL='$target' go run ./cmd/migrate   (must apply nothing)"
