#!/bin/sh
# toolkit/backup/backup-mongo.sh — full logical dump of the content DB(s).
# Owner pattern (compose_cep/overleafmongo/backup.sh: mongodump --out /backup),
# hardened: gzipped archive, canonical location, size+sha256 verify, retention.
# Runs from the host OR from the toolkit container (same docker daemon).
set -eu
cd "$(dirname "$0")"
. ./common.conf

OUT="${BACKUP_ROOT}/mongo"
mkdir -p "$OUT"
F="$OUT/${TS}.mongo.gz"
TMP="/_toolkit_mongo_${TS}.gz"   # container-internal staging

log "mongo: dumping from container $OLLITEX_MONGO -> $F"
docker exec "$OLLITEX_MONGO" mongodump --gzip --archive="$TMP" \
  || die "mongodump failed in $OLLITEX_MONGO"
sz=$(docker exec "$OLLITEX_MONGO" sh -c "wc -c < $TMP" | tr -d ' ')
[ "$sz" != "0" ] || die "mongo dump is empty (0 bytes)"
docker cp "$OLLITEX_MONGO:$TMP" "$F" >/dev/null
docker exec "$OLLITEX_MONGO" rm -f "$TMP"
# the archive must decompress to a valid gzip AND a mongo archive
gzip -t "$F" || die "mongo archive failed gzip -t"
szh=$(du -h "$F" | cut -f1)
sha=$(sha256f "$F")
log "mongo: OK ${F} (in-container ${sz} bytes -> ${szh} gz, sha256 $(printf "%s" "$sha" | cut -c1-16)…)"
retention "$OUT" "*.mongo.gz"
