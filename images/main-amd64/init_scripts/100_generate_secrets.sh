#!/bin/bash
set -e -o pipefail

# generate secrets and defines them as environment variables
# https://github.com/phusion/baseimage-docker#centrally-defining-your-own-environment-variables

WEB_API_PASSWORD_FILE=/etc/container_environment/WEB_API_PASSWORD
STAGING_PASSWORD_FILE=/etc/container_environment/STAGING_PASSWORD # HTTP auth for history-v1
V1_HISTORY_PASSWORD_FILE=/etc/container_environment/V1_HISTORY_PASSWORD
CRYPTO_RANDOM_FILE=/etc/container_environment/CRYPTO_RANDOM
OT_JWT_AUTH_KEY_FILE=/etc/container_environment/OT_JWT_AUTH_KEY

generate_secret () {
  dd if=/dev/urandom bs=1 count=32 2>/dev/null | base64 -w 0 | rev | cut -b 2- | rev | tr -d '\n+/'
}

# 2026-09-11 (R11): per-file idempotency. The old all-or-nothing block
# REGENERATED every secret when any single file was missing — and it
# OVERWROTE secrets provided via the container environment (e.g.
# CRYPTO_RANDOM from .env) with fresh random values on every container
# recreate, invalidating all stored encrypted credentials (WebDAV/Dropbox
# tokens -> 500 "error decrypting token"). Now: only create files that are
# genuinely absent; env-provided files win and are never touched.
echo "checking bootstrap secrets"
gen_into () {
  local dest="$1"
  if [ ! -f "$dest" ]; then
    echo "$(generate_secret)" > "$dest"
    echo "  generated: $dest"
  fi
}
gen_into "${WEB_API_PASSWORD_FILE}"
gen_into "${STAGING_PASSWORD_FILE}"
gen_into "${V1_HISTORY_PASSWORD_FILE}"
gen_into "${CRYPTO_RANDOM_FILE}"
gen_into "${OT_JWT_AUTH_KEY_FILE}"

