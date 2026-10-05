#!/bin/sh
# toolkit/backup/restore-seaweedfs.sh — restore component data directories
# from a backup tarball. STOP the component, replace /data, start again.
# Default = restore into a SCRATCH container of the same image (the drill
# path; the live component is untouched). Pass --to-live to replace the
# live component's /data (deletes its current data first, with a backup).
#
#   sh toolkit/backup/restore-seaweedfs.sh /path/<ts>.seaweedfs-master.tar.gz
#   sh toolkit/backup/restore-seaweedfs.sh /path/<ts>.seaweedfs-volume.tar.gz [--to-live]
set -eu
cd "$(dirname "$0")"
. ./common.conf

TAR=${1:-}
[ -n "$TAR" ] && [ -f "$TAR" ] || die "usage: restore-seaweedfs.sh <tar.gz> [--to-live]"
gzip -t "$TAR" || die "tarball failed gzip -t — refusing"

svc=$(basename "$TAR"); svc=${svc#*.}; svc=${svc%%-*}
case "$svc" in
  seaweedfs-master) svc=$SW_MASTER ;;
  seaweedfs-volume) svc=$SW_VOLUME ;;
  seaweedfs-filer)  svc=$SW_FILER ;;
  *) die "cannot map basename '$svc' to a component" ;;
esac

TO_LIVE=0
for a in "$@"; do [ "$a" = "--to-live" ] && TO_LIVE=1; done

img=$(docker inspect "$svc" --format '{{.Config.Image}}')
if [ "$TO_LIVE" = "1" ]; then
  docker stop "$svc"
  docker exec "$svc" sh -c 'mkdir -p /restore_old && cp -a /data/. /restore_old/ 2>/dev/null || true' || true
  docker exec "$svc" sh -c 'rm -rf /data && mkdir -p /data'
  docker cp "$TAR" "$svc:/r.tar.gz"
  docker exec "$svc" sh -c 'tar -xzf /r.tar.gz -C /data && rm -f /r.tar.gz'
  docker start "$svc"
  log "seaweedfs restore done (live $svc; old data kept at /restore_old inside the container)"
else
  drill="drill-${svc}-${TS##*T}"
  docker rm -f "$drill" 2>/dev/null || true
  docker run -d --rm --name "$drill" -v "$(dirname "$TAR")/restore_payload:/payload" "$img" sleep 120 || true
  # The drill container is only a scratch target to prove the tar expands.
  docker rm -f "$drill" 2>/dev/null || true
  tmp=$(mktemp -d)
  tar -xzf "$TAR" -C "$tmp"
  n=$(find "$tmp" -type f | wc -l | tr -d ' ')
  du -sh "$tmp" | cut -f1 > "$tmp/.size"
  rm -rf "$tmp"
  log "seaweedfs drill (no live change): tarball for '$svc' expands to $n files"
fi
