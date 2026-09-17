#!/usr/bin/env bash
# Daily Postgres backup (SPEC §11.5: RPO 24 h). One custom-format dump of
# the whole database, verified by listing its table of contents, kept for
# BACKUP_KEEP_DAYS. Run from cron on the database host or any host that can
# reach it; the same DATABASE_URL the api uses is enough (pg_dump needs read).
#
#   DATABASE_URL       postgres://…            (required)
#   BACKUP_DIR         where dumps go          (default /var/backups/atp)
#   BACKUP_KEEP_DAYS   retention               (default 14)
#   PG_BIN             directory of pg_dump    (default: PATH)
#
# Copy the newest file off-host afterwards (object storage with versioning);
# a backup on the same disk as the database is not a backup. test/restore/
# is the drill that proves a dump made this way comes back whole — run it
# before Gate-1 and after every migration that adds a table.
set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL is required}"
BACKUP_DIR="${BACKUP_DIR:-/var/backups/atp}"
BACKUP_KEEP_DAYS="${BACKUP_KEEP_DAYS:-14}"
PG_DUMP="${PG_BIN:+$PG_BIN/}pg_dump"
PG_RESTORE="${PG_BIN:+$PG_BIN/}pg_restore"

mkdir -p "$BACKUP_DIR"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
out="$BACKUP_DIR/atp-$stamp.dump"
tmp="$out.partial"

start=$(date +%s)
"$PG_DUMP" --format=custom --compress=6 --no-owner --no-privileges --file="$tmp" --dbname="$DATABASE_URL"
# A dump that cannot be listed cannot be restored: refuse to keep it. The
# table of contents goes to a file (--file), not a pipe: pg_restore on some
# platforms cannot write its listing into a pipe.
"$PG_RESTORE" --list --file="$tmp.toc" "$tmp"
mv "$tmp" "$out"
took=$(( $(date +%s) - start ))

# schema_migrations must be in there, or the next deploy re-runs history.
if ! grep -q 'TABLE DATA public schema_migrations' "$tmp.toc"; then
  rm -f "$tmp.toc" "$out"
  echo "backup: dump has no schema_migrations data — refusing to keep it" >&2
  exit 1
fi

rm -f "$tmp.toc"
find "$BACKUP_DIR" -name 'atp-*.dump' -type f -mtime +"$BACKUP_KEEP_DAYS" -delete
size=$(du -h "$out" | cut -f1)
echo "backup: $out ($size, ${took}s); kept $(find "$BACKUP_DIR" -name 'atp-*.dump' | wc -l) dumps"
