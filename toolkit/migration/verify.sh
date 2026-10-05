#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# verify.sh — prove the import actually landed (store + filestore + optional app)
# Exit 0 when everything required is present; non-zero otherwise.
# ─────────────────────────────────────────────────────────────────────────────
set -Eeuo pipefail
cd "$(dirname "$0")"
# shellcheck source=./common.conf
. ./common.conf

FAIL=0
ok()   { log "  ✔ $*"; }
bad()  { warn "  ✘ $*"; FAIL=1; }

q() { docker exec "${MONGO_CONTAINER}" mongosh --quiet --eval "$1" 2>/dev/null | tail -1; }

log "verifying canonical mongo (${MONGO_CONTAINER}):"
COLS=$(q "db.getSiblingDB(\"${OLLITEX_DB}\").getCollectionNames().length")
USERS=$(q "db.getSiblingDB(\"${OLLITEX_DB}\").users.countDocuments({})")
PROJECTS=$(q "db.getSiblingDB(\"${OLLITEX_DB}\").projects.countDocuments({})")
LEGACYCOLS=$(q "db.getSiblingDB(\"${LEGACY_DB}\").getCollectionNames().length")
if [ "${COLS:-0}" -ge 1 ] && [ "${USERS:-0}" -ge 1 ]; then
  ok "new '${OLLITEX_DB}' DB populated: ${COLS} collections, ${USERS} users, ${PROJECTS} projects"
else
  bad "new '${OLLITEX_DB}' DB not populated (colls=${COLS:-?} users=${USERS:-?})"
fi
if [ -n "${LEGACYCOLS}" ] && [ "${LEGACYCOLS}" -ge 1 ]; then
  ok "legacy '${LEGACY_DB}' DB intact (${LEGACYCOLS} collections) — untouched by design"
else
  warn "legacy '${LEGACY_DB}' DB not visible from this container (may be expected on a fresh box)"
fi

log "verifying legacy file content in SeaweedFS (${S3_ENDPOINT#http://} / bucket ${S3_IMPORT_BUCKET:-ollitex-legacy}):"
if command -v python3 >/dev/null 2>&1 && python3 -c 'import boto3' >/dev/null 2>&1; then
  S3_ENDPOINT="${S3_ENDPOINT:-http://127.0.0.1:${SEAWEEDFS_S3_PORT:-8333}}" BUCKET="${S3_IMPORT_BUCKET:-ollitex-legacy}" python3 - <<'PY' || FAIL=1
import boto3, botocore.client, os
s3 = boto3.client("s3", endpoint_url=os.environ["S3_ENDPOINT"].rstrip("/"),
    aws_access_key_id="x", aws_secret_access_key="x",
    config=botocore.client.Config(s3={"addressing_style":"path"}, signature_version="s3v4"),
    region_name="us-east-1")
total, size = 0, 0
p = {}
while True:
    p = s3.list_objects_v2(Bucket=os.environ["BUCKET"],
        **({"ContinuationToken": p["NextContinuationToken"]} if p.get("NextContinuationToken") else {}))
    for o in p.get("Contents", []):
        total += 1
        size += o.get("Size", 0)
    if not p.get("IsTruncated"):
        break
if total >= 1:
    print(f"  [ok ] bucket '{os.environ['BUCKET']}': {total} objects, {size/(1<<20):.0f} MiB")
else:
    print("  [FAIL] import bucket is empty")
    raise SystemExit(1)
PY
else
  warn "  (boto3 not available — S3 content verification skipped)"
fi

if docker inspect "${WEB_CONTAINER}" >/dev/null 2>&1; then
  ST=$(docker inspect "${WEB_CONTAINER}" --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' 2>/dev/null)
  if [ "${ST}" = "healthy" ]; then ok "web app healthy (${WEB_CONTAINER})"; else bad "web app state=${ST}"; fi
fi

echo
if [ "${FAIL}" -eq 0 ]; then log "VERIFY: PASS"; else log "VERIFY: FAIL"; fi
exit "${FAIL}"
