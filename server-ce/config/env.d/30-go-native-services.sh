# 30-go-native-services.sh
#
# Baked defaults for the Go-native service set (2026-09-30, live psintern
# audit — "editor dead" arc). Every value is ${VAR:-...}-guarded so an
# explicit container/compose env (or an operator-written later fragment)
# always wins; this only FILLs what the current image actually lacks.
#
# Why each group exists (live-verified 2026-09-30):
#
# 1. History-layer flips. This image ships the Go project-history (V2
#    :3054) + history-v1 (V1 :3100) and NO Node history services; with the
#    flips off both runit scripts sleep and every web history route 500s
#    (editor boot calls POST /project/<id>/flush).
# 2. History vendor data-plane. historyv1/project-history read the
#    vendor chain (OVERLEAF_MONGO_URL config-DB key || MONGO_CONNECTION_
#    STRING || MONGO_HOST:27017/sharelatex; REDIS_HOST/REDIS_PORT). The
#    docker-compose service name (`overleafmongo`) is not inherited into
#    runit service env by the vendor defaults -> without these the first
#    connect dials 127.0.0.1:27017 and fail-fasts.
# 3. Storage backends. filestore/docstore Go binaries are S3-backed (fs
#    backend retired, G2 STOR-1); without BACKEND=s3 + endpoint they
#    crash-loop (every editor file op + compile sync breaks). The endpoint
#    default (host-gateway :8333) matches the toolkit SeaweedFS stack in
#    single-host dev; production compose sets its own S3 endpoint.
# 4. CLSI working dirs. The Go CLSI defaults point at /clsi/* which does
#    not exist/is unwritable in this image layout; the correct base is the
#    /var/lib/overleaf data volume, aligned 1:1 with the operator-set
#    SANDBOXED_COMPILES_HOST_DIR_* host paths (sandbox bind mapping).

# 1. history-layer flips
export PROJECT_HISTORY_GO="${PROJECT_HISTORY_GO:-1}"
export HISTORY_V1_GO="${HISTORY_V1_GO:-1}"

# 2. history vendor data-plane
export MONGO_HOST="${MONGO_HOST:-overleafmongo}"
export MONGO_CONNECTION_STRING="${MONGO_CONNECTION_STRING:-mongodb://overleafmongo:27017/sharelatex}"
export OVERLEAF_MONGO_URL="${OVERLEAF_MONGO_URL:-mongodb://overleafmongo:27017/sharelatex}"
export REDIS_HOST="${REDIS_HOST:-overleafredis}"
export REDIS_PORT="${REDIS_PORT:-6379}"
export LOG_LEVEL="${LOG_LEVEL:-info}"

# 3. storage backends (S3 / SeaweedFS toolkit convention)
# NOTE (2026-09-30 live fix): the toolkit SeaweedFS stack has NO IAM database
# — anonymous requests are full-access, while ANY Authorization header gets
# checked against the (empty) IAM store and 403s bucket auto-creation
# ("requires Admin permission" — the 2026-09-30 12:30 frog.jpg blob-write
# failure). So credentials default to EMPTY (anonymous) here; operators with
# a real S3 gateway that requires auth still win via explicit compose env.
export OVERLEAF_FILESTORE_BACKEND="${OVERLEAF_FILESTORE_BACKEND:-s3}"
export OVERLEAF_FILESTORE_S3_ENDPOINT="${OVERLEAF_FILESTORE_S3_ENDPOINT:-http://172.17.0.1:8333}"
export AWS_S3_ENDPOINT="${AWS_S3_ENDPOINT:-http://172.17.0.1:8333}"
export BACKEND="${BACKEND:-s3}"
export BUCKET_NAME="${BUCKET_NAME:-archives}"
export TEMPLATE_FILES_BUCKET_NAME="${TEMPLATE_FILES_BUCKET_NAME:-template}"
export OVERLEAF_HISTORY_PROJECT_BLOBS_BUCKET="${OVERLEAF_HISTORY_PROJECT_BLOBS_BUCKET:-projectblobs}"
export OVERLEAF_HISTORY_BLOBS_BUCKET="${OVERLEAF_HISTORY_BLOBS_BUCKET:-global}"

# 4. CLSI working dirs (container side of the SANDBOXED_COMPILES_HOST_DIR_* mapping)
export CLSI_COMPILES_PATH="${CLSI_COMPILES_PATH:-/var/lib/overleaf/data/compiles}"
export CLSI_OUTPUT_PATH="${CLSI_OUTPUT_PATH:-/var/lib/overleaf/data/output}"
export CLSI_CACHE_PATH="${CLSI_CACHE_PATH:-/var/lib/overleaf/data/cache}"
export CLSI_UPLOAD_PATH="${CLSI_UPLOAD_PATH:-/var/lib/overleaf/data/uploads}"
