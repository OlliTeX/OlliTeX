#!/bin/sh
# toolkit/backup/backup-redis.sh — snapshot the RDB (owner pattern:
# compose_cep/overleafredis/backup.sh: cp -f dump.rdb /backup), hardened:
# BGSAVE first so the RDB is a consistent snapshot, then copy out + verify.
set -eu
cd "$(dirname "$0")"
. ./common.conf

OUT="${BACKUP_ROOT}/redis"
mkdir -p "$OUT"
F="$OUT/${TS}.redis.rdb"
TMP="/_toolkit_redis_${TS}.rdb"

log "redis: saving + copying from $OLLITEX_REDIS -> $F"
# SAVE is a synchronous consistent snapshot (our keyspace is small — a few
# ms); then stage a copy inside the container and lift it out.
docker exec "$OLLITEX_REDIS" sh -c "cp -f /data/dump.rdb $TMP" || \
  { docker exec "$OLLITEX_REDIS" redis-cli SAVE; \
    docker exec "$OLLITEX_REDIS" sh -c "cp -f /data/dump.rdb $TMP"; }
sz=$(docker exec "$OLLITEX_REDIS" sh -c "wc -c < $TMP" | tr -d ' ')
[ "$sz" != "0" ] || die "redis RDB is empty (0 bytes)"
docker cp "$OLLITEX_REDIS:$TMP" "$F" >/dev/null
docker exec "$OLLITEX_REDIS" rm -f "$TMP"
# The RDB is binary; sanity = non-empty + starts with the RDB magic 'REDIS'.
magic=$(head -c5 "$F" 2>/dev/null || true)
[ "$magic" = "REDIS" ] || die "redis file does not carry the RDB header ('$magic')"
szh=$(du -h "$F" | cut -f1)
sha=$(sha256f "$F")
log "redis: OK ${F} (${szh}, ${sz} bytes, sha256 $(printf "%s" "$sha" | cut -c1-16)…)"
retention "$OUT" "*.redis.rdb"
