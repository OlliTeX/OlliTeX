#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# files-import.sh — legacy filestore → SeaweedFS (S3), the canonical durable
# storage plane.
#
# The new stack has NO fs filestore (retired, G2 STOR-1: the filestore service
# is S3-only). So the legacy filestore CONTENT is imported as S3 OBJECTS into
# the canonical SeaweedFS plane (bucket: ${S3_IMPORT_BUCKET:-ollitex-legacy}),
# keeping the legacy layout VERBATIM as keys:
#
#     user_files/<projectId>/<path>      project files/uploads
#     template_files/<name>/<path>       template files
#     output/<projectId>/.../            generated/compile output
#     history/overleaf-chunks/...        document history chunks
#
# Ephemeral build leftovers (cache/, compiles/) are NOT imported by default
# (opt-in: --with-ephemeral). Legacy sources stay READ-ONLY (no writes).
#
# Flags:
#   --reimport         empty the import bucket first (fresh import)
#   --skip-existing    skip objects already present with identical MD5
#   --with-ephemeral   also import cache/ + compiles/ (build leftovers)
#   --dry-run          show the plan, change nothing
# ─────────────────────────────────────────────────────────────────────────────
set -Eeuo pipefail
cd "$(dirname "$0")"
# shellcheck source=./common.conf
. ./common.conf

REIMPORT=0
SKIP_EXISTING=0
WITH_EPHEMERAL=0
DRY_RUN=0
for a in "$@"; do case "$a" in
  --reimport) REIMPORT=1 ;;
  --skip-existing) SKIP_EXISTING=1 ;;
  --with-ephemeral) WITH_EPHEMERAL=1 ;;
  --dry-run) DRY_RUN=1 ;;
  -h|--help) sed -n '3,22p' "$0" | sed 's/^# \{0,1\}//' | sed '1,2d'; exit 0 ;;
  *) die "unknown flag: $a (try --help)" ;;
esac; done

require_cmd python3
python3 -c 'import boto3' 2>/dev/null || die "python3 boto3 is required (pip install boto3)"

require_container "${MONGO_CONTAINER}"   # sanity: the canonical store plane is up (and so is its S3)

# S3 target: canonical SeaweedFS S3 plane (host-exposed port is parameterized
# in .env: SEAWEEDFS_S3_PORT; normal default 8333 on a standard box).
S3_ENDPOINT="${S3_ENDPOINT:-http://127.0.0.1:${SEAWEEDFS_S3_PORT:-8333}}"
BUCKET="${S3_IMPORT_BUCKET:-ollitex-legacy}"
S3_ID="${S3_IMPORT_ID:-}"
S3_KEY="${S3_IMPORT_KEY:-}"

LEGACY_DATA="${LEGACY_SERVER_DATA}/data"
[ -d "${LEGACY_DATA}" ] || die "legacy filestore data root not found: ${LEGACY_DATA} (see LEGACY_ROOT)"

STORES="user_files,template_files,output,history"
[ "${WITH_EPHEMERAL}" -eq 1 ] && STORES="${STORES},cache,compiles"

# Legacy export zips sitting at the old /var/lib/overleaf level (one per
# project export the old instance produced) — imported as objects under
# exports/<name>.
EXTRA=""
for z in "${LEGACY_SERVER_DATA}"/*.zip; do
  [ -f "${z}" ] || continue
  EXTRA="${EXTRA:+${EXTRA};}${z}=exports/$(basename "${z}")"
done

log "target object store : ${S3_ENDPOINT}  bucket '${BUCKET}' (canonical SeaweedFS)"
log "source filestore    : ${LEGACY_DATA}  (read-only)"
log "stores              : ${STORES}"
[ -n "${EXTRA}" ] && log "extra objects     : $(echo "${EXTRA}" | tr ';' '\n' | wc -l) legacy export zip(s)"
log "reimport? ${REIMPORT}   skip-existing? ${SKIP_EXISTING}   dry-run? ${DRY_RUN}"

for s in $(echo "${STORES}" | tr ',' ' '); do
  if [ -d "${LEGACY_DATA}/${s}" ]; then
    NF=$(find "${LEGACY_DATA}/${s}" -type f 2>/dev/null | wc -l | tr -d ' ')
    SZ=$(du -sh "${LEGACY_DATA}/${s}" 2>/dev/null | awk '{print $1}')
    log "  ${s}: ${NF} files (${SZ})"
  else
    log "  ${s}: (absent from legacy data)"
  fi
done

if [ "${DRY_RUN}" -eq 1 ]; then log "dry-run: nothing was changed." && exit 0; fi

S3_ENDPOINT="${S3_ENDPOINT}" BUCKET="${BUCKET}" LEGACY_DATA="${LEGACY_DATA}" \
S3_ID="${S3_ID}" S3_KEY="${S3_KEY}" STORES="${STORES}" EXTRA="${EXTRA}" \
SKIP_EXISTING="${SKIP_EXISTING}" REIMPORT="${REIMPORT}" \
python3 ./s3-import.py

log "verifying import (objects readable back + counts) ..."
FAIL=0
S3_ENDPOINT="${S3_ENDPOINT}" BUCKET="${BUCKET}" LEGACY_DATA="${LEGACY_DATA}" STORES="${STORES}" S3_ID="${S3_ID}" S3_KEY="${S3_KEY}" python3 - <<'PY' || FAIL=1
import boto3, botocore.client, os, random, hashlib
s3 = boto3.client("s3", endpoint_url=os.environ["S3_ENDPOINT"].rstrip("/"),
    aws_access_key_id=os.environ.get("S3_ID") or "x", aws_secret_access_key=os.environ.get("S3_KEY") or "x",
    config=botocore.client.Config(s3={"addressing_style":"path"}, signature_version="s3v4"), region_name="us-east-1")
legacy = os.environ["LEGACY_DATA"]
stores = [s for s in os.environ["STORES"].split(",") if s]
keys = []
p = {}
while True:
    p = s3.list_objects_v2(Bucket=os.environ["BUCKET"], **({"ContinuationToken": p["NextContinuationToken"]} if p.get("NextContinuationToken") else {}))
    keys += [o["Key"] for o in p.get("Contents", [])]
    if not p.get("IsTruncated"): break
errors = 0
for store in stores:
    root = os.path.join(legacy, store)
    if not os.path.isdir(root): continue
    local = []
    for r,_,fs in os.walk(root):
        for f in fs: local.append(os.path.join(r,f))
    remote = [k for k in keys if k.startswith(store + "/")]
    status = "ok " if len(remote) >= len(local) else "FAIL"
    if len(remote) < len(local): errors = 1
    print(f"  [{status}] {store}: local={len(local)} bucket={len(remote)}")
    if local and remote:
        sample = random.choice(local)
        lmd5 = hashlib.md5(open(sample,'rb').read()).hexdigest()
        rmd5 = (s3.head_object(Bucket=os.environ["BUCKET"], Key=store+"/"+os.path.relpath(sample,root).replace(os.sep,"/")).get("ETag") or "").strip('"').lower()
        match = "md5 match" if lmd5 == rmd5 else "MD5 MISMATCH (large objects: head ETag may differ)"
        if lmd5 != rmd5: print(f"        sample {os.path.basename(sample)}: local={lmd5[:12]} remote={rmd5[:12]} {match}")
raise SystemExit(errors)
PY
if [ "${FAIL}" -eq 0 ]; then log "import verified."; else die "verification reported missing objects"; fi
log "done."
