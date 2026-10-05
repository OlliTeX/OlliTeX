#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# redis-import.sh — (OPTIONAL) restore the legacy overleafredis session data
#
# The app's Redis holds EPHEMERAL state only (sessions, pub/sub, queues,
# cache). Importing the legacy RDB replaces the current canonical RDB, so:
#   * OFF unless explicitly requested (--yes is required, no exceptions)
#   * a timestamped backup of the current canonical dump.rdb is kept first
#
# Usage:
#   redis-import.sh --yes       backup current RDB, install legacy RDB, restart
#   redis-import.sh             (no --yes) print the consequences and exit 0
# ─────────────────────────────────────────────────────────────────────────────
set -Eeuo pipefail
cd "$(dirname "$0")"
# shellcheck source=./common.conf
. ./common.conf

YES=0
for a in "$@"; do
  case "$a" in
    --yes) YES=1 ;;
    -h|--help) grep '^# ' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $a (see --help)" ;;
  esac
done

require_cmd docker
require_container "${REDIS_CONTAINER}"
[ -f "${LEGACY_REDIS_DUMP}" ] || die "legacy RDB not found: ${LEGACY_REDIS_DUMP}"

log "legacy RDB    : ${LEGACY_REDIS_DUMP}"
log "canonical     : ${REDIS_CONTAINER} /data (current RDB will be REPLACED)"
log "consequence   : current session/queue state is lost (ephemeral by design);"
log "                a backup of the current RDB is written to ${PREFIX}/data/redis/ first."

if [ "${YES}" -ne 1 ]; then
  log "not requested (--yes missing) — skipping. This is the default."
  exit 0
fi

TS=$(date +%Y%m%dT%H%M%S)
BACKUP_DIR="${PREFIX}/data/redis"
mkdir -p "${BACKUP_DIR}"

log "backing up current canonical RDB ..."
if docker exec "${REDIS_CONTAINER}" sh -c 'ls /data/dump.rdb' 2>/dev/null | grep -q dump.rdb; then
  docker cp "${REDIS_CONTAINER}:/data/dump.rdb" "${BACKUP_DIR}/pre-redis-import-${TS}.rdb"
  log "  backup: ${BACKUP_DIR}/pre-redis-import-${TS}.rdb"
fi

log "installing legacy RDB (file + ownership first, then a clean restart) ..."
TS=$(date +%Y%m%dT%H%M%S)
RU=$(docker exec "${REDIS_CONTAINER}" id -u redis 2>/dev/null | tr -d '[:space:]')
RG=$(docker exec "${REDIS_CONTAINER}" id -g redis 2>/dev/null | tr -d '[:space:]')
docker cp "${LEGACY_REDIS_DUMP}" "${REDIS_CONTAINER}:/data/dump.rdb"
if [ -n "${RU}" ]; then docker exec "${REDIS_CONTAINER}" chown "${RU}:${RG}" /data/dump.rdb 2>/dev/null || true; fi
# stop WITHOUT saving over the fresh file, then start -> the legacy RDB is loaded
docker exec "${REDIS_CONTAINER}" redis-cli shutdown nosave 2>/dev/null || true
sleep 2
docker start "${REDIS_CONTAINER}" >/dev/null
for i in $(seq 1 15); do
  if docker exec "${REDIS_CONTAINER}" redis-cli ping 2>/dev/null | grep -q PONG; then
    log "  redis is up with the imported RDB"; break
  fi
  [ "$i" -eq 15 ] && die "redis did not come up after import"
  sleep 2
done
KEYS=$(docker exec "${REDIS_CONTAINER}" redis-cli dbsize 2>/dev/null | tr -d '[:space:]')
log "  dbsize after import: ${KEYS}"
log "done."
