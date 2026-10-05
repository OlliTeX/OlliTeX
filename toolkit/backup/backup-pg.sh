#!/bin/sh
# toolkit/backup/backup-pg.sh — per-database logical dumps (configdb lives
# here) + a cluster-wide pg_dumpall for full fidelity. The dump runs inside
# the container, is written out, then compressed — exit status is captured
# without a masking pipe, and every artifact is size+gzip verified.
set -eu
cd "$(dirname "$0")"
. ./common.conf

OUT="${BACKUP_ROOT}/postgres"
mkdir -p "$OUT"

pgpsql() { docker exec "$OLLITEX_PG" psql -U "$OLLITEX_PG_USER" "$@"; }
pgdump() { # <outfile> <db>
  docker exec "$OLLITEX_PG" pg_dump -U "$OLLITEX_PG_USER" --clean --if-exists --no-owner "$2" > "$1"
}
pgdumpall() { # <outfile>
  docker exec "$OLLITEX_PG" pg_dumpall -U "$OLLITEX_PG_USER" --clean --if-exists > "$1"
}

log "pg: discovering databases in $OLLITEX_PG"
dbs=$(pgpsql -Atc "select datname from pg_database where datistemplate = false order by 1") || die "could not list databases"
[ -n "$dbs" ] || die "no databases found in $OLLITEX_PG"

for db in $dbs; do
  F="$OUT/${TS}.${db}.sql.gz"
  RAW="${OUT}/.${db}.raw.sql"
  log "pg: dumping database '$db' -> $F"
  if ! pgdump "$RAW" "$db"; then
    rc=$?
    rm -f "$RAW"
    die "pg_dump failed for '$db' (rc=$rc)"
  fi
  rawsz=$(wc -c < "$RAW" | tr -d ' ')
  [ "$rawsz" != "0" ] || { rm -f "$RAW"; die "pg dump for '$db' is empty (0 bytes)"; }
  gzip -c "$RAW" > "$F"
  rm -f "$RAW"
  gzip -t "$F" || die "pg dump for '$db' failed gzip -t"
  log "pg: OK '$db' ${F} ($(du -h "$F" | cut -f1), ${rawsz}B sql, sha256 $(sha256f "$F" | cut -c1-16)…)"
done

if [ "${PG_DUMPALL:-1}" != "0" ]; then
  F="$OUT/${TS}.all.sql.gz"
  RAW="${OUT}/.all.raw.sql"
  log "pg: dumping ALL (pg_dumpall) -> $F"
  if ! pgdumpall "$RAW"; then
    rc=$?
    rm -f "$RAW"
    die "pg_dumpall failed (rc=$rc)"
  fi
  rawsz=$(wc -c < "$RAW" | tr -d ' ')
  [ "$rawsz" != "0" ] || { rm -f "$RAW"; die "pg_dumpall is empty (0 bytes)"; }
  gzip -c "$RAW" > "$F"
  rm -f "$RAW"
  gzip -t "$F" || die "pg_dumpall failed gzip -t"
  log "pg: OK ALL ${F} ($(du -h "$F" | cut -f1), ${rawsz}B sql, sha256 $(sha256f "$F" | cut -c1-16)…)"
fi

retention "$OUT" "*.sql.gz"
