#!/bin/sh
# toolkit/backup/restore-mongo.sh — restore a mongo archive.
# SAFE MODEL: the drill path restores into a SCRATCH mongod (scratch
# container, torn down after) so the live store is NEVER touched by default.
# --to-live restores into the live container's databases (with --drop) and
# demands --yes.
#
#   sh toolkit/backup/restore-mongo.sh <archive.gz>              # drill (scratch mongod)
#   sh toolkit/backup/restore-mongo.sh <archive.gz> --to-live --yes
set -eu
cd "$(dirname "$0")"
. ./common.conf

ARCHIVE=${1:-}
[ -n "$ARCHIVE" ] && [ -f "$ARCHIVE" ] || die "usage: restore-mongo.sh <archive.gz> [--to-live --yes]"
TO_LIVE=0; YES=0
for a in "$@"; do [ "$a" = "--to-live" ] && TO_LIVE=1; [ "$a" = "--yes" ] && YES=1; done

gzip -t "$ARCHIVE" || die "archive failed gzip -t — refusing"

if [ "$TO_LIVE" = "1" ]; then
  [ "$YES" = "1" ] || die "--to-live is live-destructive (--drop); add --yes to confirm"
  STAGE="/restore_${TS}.gz"
  docker cp "$ARCHIVE" "$OLLITEX_MONGO:$STAGE"
  res=$(docker exec "$OLLITEX_MONGO" mongorestore --gzip --archive="$STAGE" --drop 2>&1) \
    || { echo "$res" | tail -5; docker exec "$OLLITEX_MONGO" rm -f "$STAGE"; die "mongorestore failed"; }
  docker exec "$OLLITEX_MONGO" rm -f "$STAGE"
  log "mongo restore into live done. mongorestore tail:"
  echo "$res" | tail -3
  exit 0
fi

# ---- drill: scratch mongod, restore, count, tear down -----------------------
IMG=$(docker inspect "$OLLITEX_MONGO" --format '{{.Config.Image}}')
drill="drill-mongo-${TS##*T}"
tmp=$(mktemp -d)
docker run -d --name "$drill" -e GLIBC_TUNABLES=glibc.cpu.hwcaps=-SHSTK -v "$tmp:/restore" "$IMG" >/dev/null
# wait for the scratch mongod
i=0
until docker exec "$drill" mongosh --quiet --eval 'db.runCommand({ping:1}).ok' >/dev/null 2>&1; do
  i=$((i+1)); [ "$i" -gt 40 ] && { docker rm -f "$drill" >/dev/null 2>&1; rm -rf "$tmp"; die "scratch mongod did not start"; }
  sleep 1
done
docker cp "$ARCHIVE" "$drill:/restore/${TS}.gz"
res=$(docker exec "$drill" mongorestore --gzip --archive="/restore/${TS}.gz" --drop 2>&1) \
  || { echo "$res" | tail -5; docker rm -f "$drill" >/dev/null 2>&1; rm -rf "$tmp"; die "drill mongorestore failed"; }
# count what came back
counts=$(docker exec "$drill" mongosh --quiet --eval '
const out = {};
const dbs = db.adminCommand({listDatabases: 1}).databases.map(d => d.name);
for (const dbn of dbs) { out[dbn] = db.getSiblingDB(dbn).getCollectionNames().length; }
print(JSON.stringify(out))')
# sample: users + projects in ollitex + sharelatex
sample=$(docker exec "$drill" mongosh --quiet --eval '
print(JSON.stringify({
  ollitex_users: db.getSiblingDB("ollitex").user.countDocuments(),
  ollitex_projects: db.getSiblingDB("ollitex").projects.countDocuments(),
  sharelatex_users: db.getSiblingDB("sharelatex").user.countDocuments(),
  sharelatex_projects: db.getSiblingDB("sharelatex").projects.countDocuments()
}))')
docker rm -f "$drill" >/dev/null 2>&1
rm -rf "$tmp"
log "mongo DRILL PASS — restored into scratch mongod and counted:"
log "  collections per db: $counts"
log "  sample counts: $sample"
echo "$res" | tail -2
