#!/bin/sh
# toolkit/backup/restore-pg.sh — apply a per-database pg dump.
#   sh restore-pg.sh <dump.sql.gz>                    # restore into its own DB (live)
#   sh restore-pg.sh <dump.sql.gz> <target-db>        # restore into a NEW database (drill)
#   sh restore-pg.sh <dump.sql.gz> --to-live <db>     # explicit live restore (default)
set -eu
cd "$(dirname "$0")"
. ./common.conf

SQL=${1:-}
[ -n "$SQL" ] && [ -f "$SQL" ] || die "usage: restore-pg.sh <dump.sql.gz> [target-db|--to-live <db>]"
shift || true

TARGET=""
TO_LIVE=1
if [ "$#" -gt 0 ]; then
  case "$1" in
    --to-live) shift; TARGET=${1:-}; TO_LIVE=1;;
    *) TARGET="$1"; TO_LIVE=1;;
  esac
fi

gunzip -t "$SQL" || die "dump failed gunzip -t — refusing"
RAW="$(mktemp)"; gunzip -c "$SQL" > "$RAW"
[ -s "$RAW" ] || { rm -f "$RAW"; die "dump is empty"; }

if [ -n "$TARGET" ]; then
  log "pg: restoring into NEW database '$TARGET'"
  docker exec "$OLLITEX_PG" createdb -U "$OLLITEX_PG_USER" "$TARGET"
  docker exec -i "$OLLITEX_PG" psql -U "$OLLITEX_PG_USER" -d "$TARGET" -v ON_ERROR_STOP=1 -f - < "$RAW" > /tmp/pgrestore.log 2>&1 \
    || { tail -20 /tmp/pgrestore.log; rm -f "$RAW"; die "restore into '$TARGET' failed"; }
  log "pg: OK — restored into '$TARGET'"
else
  log "pg: restoring into the dump's own database (LIVE)"
  db=${SQL##*.}; db=${db%%.*}; db=$(basename "$SQL" | sed -E 's/.*[._]([a-z0-9-]+)\.sql\.gz$/\1/' | head -1)
  [ -n "$db" ] || db=overleaf
  docker exec -i "$OLLITEX_PG" psql -U "$OLLITEX_PG_USER" -d "$db" -v ON_ERROR_STOP=1 -f - < "$RAW" > /tmp/pgrestore.log 2>&1 \
    || { tail -20 /tmp/pgrestore.log; rm -f "$RAW"; die "restore into '$db' failed"; }
  log "pg: OK — restored into live '$db'"
fi
rm -f "$RAW"
tail -3 /tmp/pgrestore.log | sed 's/^/[pg]   /' || true
