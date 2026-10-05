#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# mongo-import.sh — import the legacy Overleaf app DB into the NEW ollitex DB
#
# Owner model (2026-10-06): the legacy `sharelatex` database is NEVER renamed,
# dropped, or mutated. Its mongodump is RESTORED under the new database name
# `ollitex` in the canonical overleafmongo. The OlliTeX project then uses
# `ollitex`; any future Overleaf install keeps its own `sharelatex` world.
#
# Usage:
#   mongo-import.sh                 import (append-restore; idempotent enough)
#   mongo-import.sh --reimport      drop the NEW ollitex DB first, then restore
#   mongo-import.sh --dry-run       print the plan and the current state, do nothing
# ─────────────────────────────────────────────────────────────────────────────
set -Eeuo pipefail
cd "$(dirname "$0")"
# shellcheck source=./common.conf
. ./common.conf

REIMPORT=0
DRY_RUN=0
for a in "$@"; do
  case "$a" in
    --reimport) REIMPORT=1 ;;
    --dry-run)  DRY_RUN=1 ;;
    -h|--help)  grep '^# ' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown option: $a (see --help)" ;;
  esac
done

require_cmd docker
require_container "${MONGO_CONTAINER}"
preflight_safety

SRC="${LEGACY_MONGO_DUMP}/${LEGACY_DB}"
[ -d "${SRC}" ] || die "legacy dump dir not found: ${SRC}"
[ -n "$(ls "${SRC}"/*.bson 2>/dev/null)" ] || die "no .bson collections in ${SRC}"

count_of() {
  docker exec "${MONGO_CONTAINER}" mongosh --quiet --eval \
    "db.getSiblingDB(\"$1\").getCollectionNames().length" 2>/dev/null | tail -1
}

COLS_LEGACY_BEFORE=$(count_of "${LEGACY_DB}")
COLS_TARGET_BEFORE=$(count_of "${OLLITEX_DB}")
SRC_BSON_COUNT=$(ls "${SRC}"/*.bson 2>/dev/null | wc -l | tr -d ' ')

log "source dump : ${SRC}  (${SRC_BSON_COUNT} collections)"
log "target DB   : ${OLLITEX_DB}  (currently ${COLS_TARGET_BEFORE} collections)"
log "legacy DB   : ${LEGACY_DB}  (currently ${COLS_LEGACY_BEFORE} collections — will stay UNTOUCHED)"
log "reimport?   : ${REIMPORT}    dry-run? : ${DRY_RUN}"

if [ "${DRY_RUN}" -eq 1 ]; then log "dry-run: nothing done."; exit 0; fi

DROP_ARG=""
[ "${REIMPORT}" -eq 1 ] && DROP_ARG="--drop"

log "staging dump into ${MONGO_CONTAINER}:${MONGO_STAGE_DIR}/${LEGACY_DB} ..."
docker exec "${MONGO_CONTAINER}" mkdir -p "${MONGO_STAGE_DIR}"
# docker cp: src → container:dest
docker cp "${SRC}" "${MONGO_CONTAINER}:${MONGO_STAGE_DIR}/${LEGACY_DB}"

log "restoring ${LEGACY_DB} -> ${OLLITEX_DB} (mongorestore ${DROP_ARG:-append}) ..."
# `--db` retargets the single database named in `--dir` (the documented
# rename-on-restore form). `--drop` only applies to the TARGET db.
# (tools 100.x have no --logFile/--logLevel — the log stays on the host)
Restore_LOG="/tmp/ollitex-mongorestore-$$(date +%Y%m%dT%H%M%S).log"
docker exec "${MONGO_CONTAINER}" mongorestore \
  --db "${OLLITEX_DB}" \
  --dir "${MONGO_STAGE_DIR}/${LEGACY_DB}" \
  ${DROP_ARG} 2>&1 | tee "${Restore_LOG}" | tail -8
cp "${Restore_LOG}" "${PREFIX}/data/mongorestore-ollitex-last.log" 2>/dev/null || true

COLS_TARGET_AFTER=$(count_of "${OLLITEX_DB}")
USERS_AFTER=$(docker exec "${MONGO_CONTAINER}" mongosh --quiet --eval \
  "db.getSiblingDB(\"${OLLITEX_DB}\").users.countDocuments({})" 2>/dev/null | tail -1)
PROJECTS_AFTER=$(docker exec "${MONGO_CONTAINER}" mongosh --quiet --eval \
  "db.getSiblingDB(\"${OLLITEX_DB}\").projects.countDocuments({})" 2>/dev/null | tail -1)
FOLDERS_AFTER=$(docker exec "${MONGO_CONTAINER}" mongosh --quiet --eval \
  "db.getSiblingDB(\"${OLLITEX_DB}\").folders.countDocuments({})" 2>/dev/null | tail -1)

log "import result:"
log "  ${OLLITEX_DB}: ${COLS_TARGET_AFTER} collections | users=${USERS_AFTER} projects=${PROJECTS_AFTER} folders=${FOLDERS_AFTER}"

# Safety proof: the legacy DB collection count is EXACTLY as before
COLS_LEGACY_AFTER=$(count_of "${LEGACY_DB}")
if [ "${COLS_LEGACY_AFTER}" != "${COLS_LEGACY_BEFORE}" ]; then
  warn "LEGACY ${LEGACY_DB} collection count CHANGED (${COLS_LEGACY_BEFORE} -> ${COLS_LEGACY_AFTER}) — investigate!"
  exit 1
fi
log "safety: legacy ${LEGACY_DB} untouched (${COLS_LEGACY_AFTER} collections, unchanged)"

docker exec "${MONGO_CONTAINER}" rm -rf "${MONGO_STAGE_DIR}" 2>/dev/null || true
log "done."
