#!/bin/sh
# toolkit/backup/backup-seaweedfs.sh — back up each SeaweedFS component's
# data directory (master cluster-state, volume file data, filer metadata).
# Tars each container's /data out to the canonical location, then verifies.
set -eu
cd "$(dirname "$0")"
. ./common.conf

OUT="${BACKUP_ROOT}/seaweedfs"
mkdir -p "$OUT"

backup_one() {
  svc=$1
  F="$OUT/${TS}.${svc}.tar.gz"
  log "seaweedfs: tarring $svc /data -> $F"
  # tar inside the container (tar is present in the seaweedfs image),
  # stream out — no staging file to clean up.
  docker exec "$svc" tar -czf - -C /data . > "$F" || die "tar failed in $svc"
  sz=$(wc -c < "$F" | tr -d ' ')
  [ "$sz" != "0" ] || die "seaweedfs tar for $svc is empty (0 bytes)"
  gzip -t "$F" || die "seaweedfs tar for $svc failed gzip -t"
  log "seaweedfs: OK $svc ${F} ($(du -h "$F" | cut -f1), sha256 $(sha256f "$F" | cut -c1-16)…)"
}

for svc in "$SW_MASTER" "$SW_VOLUME" "$SW_FILER"; do
  # Skip components that are not running (e.g. ENABLE_SEAWEEDFS=no).
  if docker inspect "$svc" >/dev/null 2>&1; then
    backup_one "$svc"
  else
    log "seaweedfs: skip $svc (container not present)"
  fi
done

retention "$OUT" "*.tar.gz"
