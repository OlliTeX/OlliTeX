#!/bin/sh
# toolkit/backup/restore-redis.sh — swap the RDB back into the live container
# and bounce the server so it loads the snapshot. Backs up the CURRENT rdb
# first (so a bad restore is itself undoable).
#
#   sh toolkit/backup/restore-redis.sh /path/<ts>.redis.rdb [--yes]
set -eu
cd "$(dirname "$0")"
. ./common.conf

RDB=${1:-}
[ -n "$RDB" ] && [ -f "$RDB" ] || die "usage: restore-redis.sh <rdb> [--yes]"
head -c5 "$RDB" | grep -q REDIS || die "not a redis RDB (missing REDIS header)"
[ "${2:-}" = "--yes" ] || die "add --yes to confirm replacing the live keyspace"

STAGE="/restore_${TS}.rdb"
docker exec "$OLLITEX_REDIS" sh -c 'cp -f /data/dump.rdb /pre_restore_'"$TS"'.rdb'
docker cp "$RDB" "$OLLITEX_REDIS:$STAGE"
docker exec "$OLLITEX_REDIS" sh -c "mv $STAGE /data/dump.rdb"
docker restart "$OLLITEX_REDIS"
sleep 3
n=$(docker exec "$OLLITEX_REDIS" redis-cli DBSIZE)
log "redis restore done: DBSIZE=$n (pre-restore snapshot kept in-container as /pre_restore_${TS}.rdb)"
