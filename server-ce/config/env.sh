export CHAT_HOST=127.0.0.1
export CLSI_HOST=127.0.0.1
export DOCSTORE_HOST=127.0.0.1
export DOCUMENT_UPDATER_HOST=127.0.0.1
export DOCUPDATER_HOST=127.0.0.1
export FILESTORE_HOST=127.0.0.1
export HISTORY_V1_HOST=127.0.0.1
export LINKED_URL_PROXY_HOST=127.0.0.1
export NOTIFICATIONS_HOST=127.0.0.1
export PROJECT_HISTORY_HOST=127.0.0.1
export REALTIME_HOST=127.0.0.1
export WEB_HOST=127.0.0.1
export WEB_API_HOST=127.0.0.1

# 2026-09-05 (owner OlliTeX rebrand) -> 2026-09-07 (owner: LibreLeaf):
# product name shown in titles, navbar, emails and i18n (settings.appName).
# Distinct from Overleaf/ShareLaTeX per the AGPL fork branding checklist;
# attribution stays in the footer.
# Both env names are honored (server-ce settings.js reads OVERLEAF_APP_NAME,
# services/web settings.defaults.js reads APP_NAME) — set both.
export OVERLEAF_APP_NAME=${OVERLEAF_APP_NAME:-LibreLeaf}
export APP_NAME=${APP_NAME:-LibreLeaf}

# 2026-09-09 (owner R11 boot-crash fix): in this image lineage the
# /etc/container_environment exports performed by my_init do NOT reach the
# runsv services (verified live: CRYPTO_RANDOM = LOST in the web service
# env), so the web process crashed at boot with "No SESSION_SECRET
# provided" (sessionSecret = OVERLEAF_SESSION_SECRET || CRYPTO_RANDOM).
# my_init writes a shell dump of the full environment at startup; sourcing
# it here restores the bootstrap secrets (CRYPTO_RANDOM → cookie signing,
# WEB_API_USER/PASSWORD, LLM_KEY_SECRET, …) for every service that sources
# /etc/overleaf/env.sh. Idempotent and additive (never clears vars).
if [ -f /etc/container_environment.sh ]; then
  # shellcheck disable=SC1091
  . /etc/container_environment.sh
fi
