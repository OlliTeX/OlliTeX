# develop/ — OlliTeX local dev stack (Go superstack)  (2026-11-16 rewrite, TODO-d1d723f2)

## Topology (single Go image, mirrors tests/e2e and toolkit prod)
`ollitex/ollitex:main` (or `$OVERLEAF_IMAGE`) runs **web + api + clsi + real-time +
docstore + filestore + chat + notifications + configdb** (profiles inside one container).
Sidecars: mongo/redis/postgres (data volumes), minio-swarm (filestore S3), smtpsink
(repo script `tests/e2e/tools/smtp-sink.js`), git-bridge (GitHub OAuth + sync),
languagetool (CEX grammar), wakapi (stats, optional — default off).

Retired 2026-11-16 (commented, owner delete-gate):
- `web` → `junk/services-web/Dockerfile` (retired Node web build) — replaced by superstack web profile
- `webpack` (dead build stage; `webpack.config.dev-env.js` had no consumer; no
  node_modules in the image)
- `dev.env` — retired Node WEB host vars (REDIS_URL / APP_CONFIG... / NODE_OPTIONS);
  now a fixture for the optional Node `services-chat` container (default commented) +
  the real Go env contract lives in docker-compose.yml web.environment
- `bin/up` node-web rebuild branch — commented

## Quickstart
    cd develop
    bin/up        # base stack → http://127.0.0.1:18080  (web bound to 127.0.0.1)
    bin/dev       # follow logs
    bin/shell web # exec in the web container

Ports (127.0.0.1-only): 18080 web · 18500 real-time :5000 · 18800 git-bridge :3001 ·
18600 languagetool :9090 · 18300 smtpsink API (sink SMTP :1025 internal).
mongo/redis/postgres are internal to the `ollitex-dev_*` network (no host ports).

## Verified 2026-11-16 (fresh mongo, same image as the e2e world)
- web profile healthy; /login 200 · csrf gate live (400/403 without token)
- POST /register 200 → Go go-mail → smtpsink captured ("Activate your OlliTeX Account")
  → /user/activate 302 · mail-flow e2e
- login (JSON + x-csrf-token) 302 → /hub 200
- POST /project/new 200 (project_id) → GET /editor/<id> 200 (editor shell)
- KNOWN quirk (pinned in tests/e2e/specs/auth-lifecycle.test.e2e.ts, ⚑ OWNER FLAG):
  registration stores a random password — logging in with the chosen password 401s until
  the password-reset flow (Go parity behavior; dev smoke set the hash via admin path).

## Files
docker-compose.yml (base, e2e-derived) · docker-compose.dev.yml (dev overlays: ports,
optional chat) · bin/{up,dev,shell,logs,down,build} · gitbridge-runtime.json (unused
legacy) · compiles/ (shared texlive sandbox dirs = tests/e2e/local-data) · ngrams/ (cache)
