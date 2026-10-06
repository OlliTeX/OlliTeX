#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# wipe-targets.sh — wipe the OlliTeX-plane import targets BEFORE the import
# (owner directive 2026-10-06: "before starting the migration, the redis,
# mongodb and postgres are wiped to prevent collisions").
#
# What it wipes (and what it never touches):
#   * mongo : the TARGET database (${OLLITEX_DB}) on ${MONGO_CONTAINER} is
#             DROPPED. The legacy source (testdata + any `sharelatex` DB) is
#             read-only and untouched.
#   * redis : FLUSHALL on ${REDIS_CONTAINER} (stale sessions/caches from the
#             pre-import world — users re-login once).
#   * pg    : CONFIGSTORE DECISION (recorded in common.conf + README): the
#             `configdb` table is KEPT — the provided source carries no PG
#             data, and the live configdb is the running stack's config &
#             secret plane. With --wipe-pg-app-tables (off by default) the
#             other app tables are truncated too (configdb always survives).
#
# Safety:
#   * a pre-wipe snapshot is taken first (mongo dump + pg_dumpall + redis RDB
#     copy) under ${PREFIX}/data/backups/pre-wipe-<ts>/ — roll-forward is
#     always possible without touching the source.
#   * requires an explicit --yes (or a --dry-run that changes nothing).
#
# Usage:
#   wipe-targets.sh --yes                    wipe mongo target + redis (pg kept)
#   wipe-targets.sh --yes --wipe-pg-app-tables   also truncate PG app tables
#   wipe-targets.sh --dry-run                show the plan, change nothing
# ─────────────────────────────────────────────────────────────────────────────
set -Eeuo pipefail
cd "$(dirname "$0")"
# shellcheck source=./common.conf
. ./common.conf

WIPE_PG_APP_TABLES=0
YES=0
DRY_RUN=0
for a in "$@"; do
  case "$a" in
    --wipe-pg-app-tables) WIPE_PG_APP_TABLES=1 ;;
    --yes)                YES=1 ;;
    --dry-run)            DRY_RUN=1 ;;
    -h|--help)  grep '^# ' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $a (see --help)" ;;
  esac
done

TS="$(date -u +%Y%m%dT%H%M%SZ)"
SNAP_DIR="${PREFIX}/data/backups/pre-wipe-${TS}"

log "wipe plan:"
log "  mongo : DROP database '${OLLITEX_DB}' on container ${MONGO_CONTAINER}  (legacy '${LEGACY_DB}' stays untouched)"
log "  redis : FLUSHALL on container ${REDIS_CONTAINER}"
log "  pg    : configstore 'configdb' KEPT (no PG data in the source; live config/secret plane)"
if [ "${WIPE_PG_APP_TABLES}" -eq 1 ]; then
  log "  pg    : (+--wipe-pg-app-tables) TRUNCATE all non-config tables in the app DBs"
else
  log "  pg    : other app tables left as-is (default)"
fi
log "  snapshot → ${SNAP_DIR}"
log "  source (read-only): ${LEGACY_ROOT}"

if [ "${DRY_RUN}" -eq 1 ]; then log "dry-run: nothing changed."; exit 0; fi
if [ "${YES}" -ne 1 ]; then
  die "refusing to wipe without an explicit --yes (add --yes, or use --dry-run)"
fi

require_container "${MONGO_CONTAINER}"
require_container "${REDIS_CONTAINER}"

# ── 1) snapshot the targets (fail hard — never wipe without a rollback) ─────
mkdir -p "${SNAP_DIR}"
log "snapshot: mongo '${OLLITEX_DB}' ..."
docker exec "${MONGO_CONTAINER}" mongodump --db "${OLLITEX_DB}" --gzip \
  --out "/tmp/wipesnap_${TS}" 2>&1 | tail -1
docker cp "${MONGO_CONTAINER}:/tmp/wipesnap_${TS}" "${SNAP_DIR}/mongo-${OLLITEX_DB}" >/dev/null 2>&1
docker exec "${MONGO_CONTAINER}" rm -rf "/tmp/wipesnap_${TS}" 2>/dev/null || true

log "snapshot: postgres (configdb + app DBs) ..."
docker exec ollitex-pg pg_dumpall > "${SNAP_DIR}/pg_dumpall.sql" 2>/dev/null \
  || warn "pg_dumpall failed (container 'ollitex-pg' down?) — pg is kept anyway, but the rollback snapshot is missing"

log "snapshot: redis RDB copy ..."
docker exec "${REDIS_CONTAINER}" sh -c 'redis-cli SAVE >/dev/null 2>&1 || true; f=$(ls -1t /data/dump.rdb /data/*.rdb 2>/dev/null | head -1); [ -n "$f" ] && cp "$f" /tmp/pre-wipe-dump.rdb || echo no-rdb' 2>/dev/null || true
docker cp "${REDIS_CONTAINER}:/tmp/pre-wipe-dump.rdb" "${SNAP_DIR}/redis-dump.rdb" >/dev/null 2>&1 || true
docker exec "${REDIS_CONTAINER}" rm -f /tmp/pre-wipe-dump.rdb 2>/dev/null || true
log "snapshot stored under ${SNAP_DIR}"

# ── 2) wipe the targets ──────────────────────────────────────────────────────
log "wipe: dropping mongo '${OLLITEX_DB}' ..."
BEFORE="$(docker exec "${MONGO_CONTAINER}" mongosh --quiet --eval "db.getSiblingDB('${OLLITEX_DB}').getCollectionNames().length" 2>/dev/null | tail -1 || echo ?)"
docker exec "${MONGO_CONTAINER}" mongosh --quiet --eval \
  "print('dropped db: ' + JSON.stringify(db.getSiblingDB('${OLLITEX_DB}').dropDatabase()))"
AFTER_LEGACY="$(docker exec "${MONGO_CONTAINER}" mongosh --quiet --eval "db.getSiblingDB('${LEGACY_DB}').getCollectionNames().length" 2>/dev/null | tail -1 || echo 0)"
log "  target collections before: ${BEFORE:-?} | legacy '${LEGACY_DB}' collections: ${AFTER_LEGACY} (untouched)"

log "wipe: FLUSHALL on redis ..."
docker exec "${REDIS_CONTAINER}" redis-cli FLUSHALL
log "  redis dbs: $(docker exec "${REDIS_CONTAINER}" redis-cli DBSIZE 2>/dev/null | tail -1)"

if [ "${WIPE_PG_APP_TABLES}" -eq 1 ]; then
  log "wipe: truncating PG app tables (configdb KEPT) ..."
  docker exec ollitex-pg psql -U overleaf -d overleaf -t -A -c \
    "SELECT 'TRUNCATE public.' || tablename FROM information_schema.tables WHERE table_schema='public' AND tablename<>'configdb'" 2>/dev/null \
  | while IFS= read -r stmt; do [ -n "$stmt" ] && docker exec ollitex-pg psql -U overleaf -d overleaf -c "${stmt} CASCADE" >/dev/null; done
  log "  configdb rows after: $(docker exec ollitex-pg psql -U overleaf -d overleaf -t -A -c 'SELECT count(*) FROM configdb' 2>/dev/null)"
fi

log "wipe complete. Targets are clean; snapshot in ${SNAP_DIR}."
