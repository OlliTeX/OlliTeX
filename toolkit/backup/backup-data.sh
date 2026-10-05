#!/bin/sh
# toolkit/backup/backup-data.sh — the canonical data-backup round for the
# OlliTeX stack: mongo + redis + postgres + SeaweedFS, each verified
# (non-empty + integrity) and retention-pruned. Host-side or TUI-side
# (same docker daemon); output → ${PREFIX}/data/backups/<store>/<ts>.<ext>.
#
# Usage:
#   sh toolkit/backup/backup-data.sh            # all stores
#   sh toolkit/backup/backup-data.sh mongo pg   # a subset
# Env:
#   BACKUP_ROOT     (default ${PREFIX}/data/backups)
#   RETENTION       (default 7 newest per store)
#   PG_DUMPALL=0    skip the cluster-wide pg_dumpall
set -u
cd "$(dirname "$0")"
. ./common.conf
export BACKUP_ROOT TS RETENTION

targets="mongo redis postgres seaweedfs"
if [ "$#" -gt 0 ]; then targets="$*"; fi

fail=0
for t in $targets; do
  case "$t" in
    mongo)      sh ./backup-mongo.sh      || { log "mongo: FAILED"; fail=1; } ;;
    redis)      sh ./backup-redis.sh      || { log "redis: FAILED"; fail=1; } ;;
    postgres|pg) sh ./backup-pg.sh        || { log "postgres: FAILED"; fail=1; } ;;
    seaweedfs)  sh ./backup-seaweedfs.sh  || { log "seaweedfs: FAILED"; fail=1; } ;;
    all)        for x in mongo redis postgres seaweedfs; do
                   sh ./backup-$([ "$x" = pg ] && echo pg || echo $x).sh || { log "$x: FAILED"; fail=1; }
                 done ;;
    *) log "unknown target '$t' (choose: mongo redis postgres seaweedfs all)"; fail=1 ;;
  esac
done

log "round complete (root: $BACKUP_ROOT)"
if [ -d "$BACKUP_ROOT" ]; then
  du -sh "$BACKUP_ROOT"/* 2>/dev/null | sed 's/^/  /'
fi
[ "$fail" = "0" ] || exit 1
exit 0
