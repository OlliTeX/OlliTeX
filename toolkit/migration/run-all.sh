#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# run-all.sh — the full legacy → OlliTeX import in a safe, auditable order.
#
#   1. preflight (container up, sources present, safety names distinct)
#   2. mongo  : legacy `sharelatex` dump -> NEW `ollitex` DB   (mongo-import.sh)
#   3. files  : legacy filestore → SeaweedFS (S3) bucket (files-import.sh)
#   4. verify : the store + filestore actually hold the data   (verify.sh)
#   (redis is EPHEMERAL and is NOT touched unless you pass --with-redis --yes)
#
# Usage:
#   run-all.sh                     mongo + files + verify (safe defaults)
#   run-all.sh --reimport          drop the NEW ollitex DB / wipe web dir first
#   run-all.sh --dry-run           plan only, change nothing
#   run-all.sh --with-redis --yes  ALSO replace the canonical RDB (see redis-import.sh)
# ─────────────────────────────────────────────────────────────────────────────
set -Eeuo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "${HERE}"

REIMPORT=""
DRY_RUN=""
WITH_REDIS=0
RD_YES=0
WITH_EPHEMERAL=0
for a in "$@"; do
  case "$a" in
    --reimport)         REIMPORT="--reimport" ;;
    --dry-run)          DRY_RUN="--dry-run" ;;
    --with-redis)       WITH_REDIS=1 ;;
    --with-ephemeral)   WITH_EPHEMERAL=1 ;;
    --yes)              RD_YES=1 ;;
    -h|--help)      grep '^# ' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $a" >&2; exit 2 ;;
  esac
done

if [ "${WITH_REDIS}" -eq 1 ] && [ "${RD_YES}" -ne 1 ]; then
  echo "error: --with-redis requires an explicit --yes (it replaces the canonical RDB)." >&2
  exit 2
fi

echo
echo "════════════════════════════════════════════════════════════════════"
echo " OlliTeX legacy import  (canonical stack targets from common.conf)"
echo "════════════════════════════════════════════════════════════════════"

# shellcheck source=./common.conf
. ./common.conf
print_config
preflight_safety

log "── step 1: mongo ──"
bash "${HERE}/mongo-import.sh" ${REIMPORT} ${DRY_RUN}

log "── step 2: files (→ SeaweedFS) ──"
FA=""
[ -n "${REIMPORT}" ] && FA="${FA} --reimport"
[ -n "${DRY_RUN}" ] && FA="${FA} --dry-run"
[ "${WITH_EPHEMERAL}" -eq 1 ] && FA="${FA} --with-ephemeral"
bash "${HERE}/files-import.sh" ${FA}

[ -n "${DRY_RUN}" ] && { log "dry-run: nothing was changed."; exit 0; }

log "── step 3: redis (optional) ──"
if [ "${WITH_REDIS}" -eq 1 ]; then
  bash "${HERE}/redis-import.sh" --yes
else
  log "  skipped (ephemeral; add --with-redis --yes to change the canonical RDB)"
fi

log "── step 4: verify ──"
bash "${HERE}/verify.sh"

echo
log "ALL IMPORT STEPS COMPLETE. The OlliTeX web plane uses the new '${OLLITEX_DB}' DB; the legacy file content now lives in SeaweedFS (bucket '${S3_IMPORT_BUCKET}')."
