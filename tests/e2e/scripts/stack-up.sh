#!/usr/bin/env bash
# stack-up.sh — idempotent bring-up of the disposable e2e stack.
# Regenerates bootstrap secrets when starting a fresh stack, brings
# sidecars + overleaf + git-bridge up, waits for /login to answer 200.
set -euo pipefail
cd "$(dirname "$0")/.."

PROJECT=ol-e2e
BASE_URL="http://127.0.0.1:7420"
DC=(docker compose -f docker-compose.test.yml --env-file .env.test --project-name "$PROJECT")

if curl -fs -o /dev/null -m 3 "${BASE_URL}/login" 2>/dev/null; then
  echo "[stack] already running at ${BASE_URL} (reusing)"
  exit 0
fi

# fresh bootstrap secrets (test-only values; the stack is disposable)
gen_hex() { openssl rand -hex 32; }
gen_b64() { openssl rand -base64 32; }
{
  echo "CRYPTO_RANDOM=$(gen_hex)"
  echo "WEB_API_PASSWORD=$(gen_hex)"
  echo "SHARED_SERVICE_TOKEN=$(gen_hex)"
  echo "OT_JWT_AUTH_KEY=$(gen_hex)"
  echo "OVERLEAF_SESSION_SECRET=$(gen_hex)"
  echo "LLM_KEY_SECRET=$(gen_hex)"
  echo "OVERLEAF_INVITE_TOKEN_SECRET=$(openssl rand -base64 32)"
} > .env.test
echo "[stack] generated fresh bootstrap secrets (.env.test, gitignored)"

# 2026-09 (PDF-404 root fix): the sandboxed-compile dirs are bind-mounted at
# /var/lib/overleaf/data/{compiles,cache,output} (SAME PATH on host, per the
# docker-daemon contract, AND the path the image's baked clsi-nginx.conf
# serves output from). Pre-create them owned by the texlive image user (uid
# 33) so the sandbox container can write; the docker daemon would
# otherwise create them root-owned and compiles would fail with EACCES.
for d in compiles cache output; do
  mkdir -p "/var/lib/overleaf/data/$d" 2>/dev/null || true
  chmod 777 "/var/lib/overleaf/data/$d" 2>/dev/null || true
  chown 33:33 "/var/lib/overleaf/data/$d" 2>/dev/null || true

done

"${DC[@]}" up -d

echo "[stack] waiting for /login (timeout 900s)..."
deadline=$(( $(date +%s) + 900 ))
until curl -fs -o /dev/null -m 5 "${BASE_URL}/login" 2>/dev/null; do
  if (( $(date +%s) > deadline )); then
    "${DC[@]}" logs --tail 40 overleaf || true
    echo "[stack] ERROR: overleaf did not become healthy in 900s" >&2
    exit 1
  fi
  sleep 5
done
echo "[stack] overleaf is up at ${BASE_URL}"
