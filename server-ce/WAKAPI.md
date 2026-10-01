# Wakapi (self-hosted WakaTime server) in the default OlliTeX stack

Audit 035 (2026-09-30): the WakaTime integration (editor heartbeat relay →
Go web `/user/wakatime` routes) needs a WakaTime-compatible backend. The
default stack now ships one (Wakapi, `ghcr.io/muety/wakapi`), so a fresh
OlliTeX instance has working WakaTime out of the box.

## Where

| Stack | File | Service | Endpoint |
|-------|------|---------|----------|
| production default | `docker-compose.yml` | `wakapi` | host `:5001` → container `:3000` |
| dev | `develop/docker-compose.yml` | `wakapi` | `127.0.0.1:5001` → container `:3000` (also `http://wakapi:3000` on the compose network) |

## Configuration contract (official muety/wakapi quick start)

- **Persistence:** the SQLite data lives on a dedicated volume at `/data`
  (`/data/wakapi.db`) — `~/wakapi_data` (production, `WAKAPI_DATA_DIR`
  override) / `./data/wakapi` (dev). Recreating the container keeps all
  Wakapi state (users, API keys, heartbeats).
- **Generated salt:** `WAKAPI_PASSWORD_SALT` is the password-hashing salt —
  generate at deploy time (official pattern):
  ```sh
  SALT="$(cat /dev/urandom | LC_ALL=C tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n 1)"
  echo "WAKAPI_PASSWORD_SALT=$SALT" >> deploy.env
  ```
  The compose files leave it empty by default (Wakapi then uses its own
  generated value) — never bake a weak/static salt into the repo.
- **Bootstrap admin:** `WAKAPI_BOOTSTRAP_ADMIN_EMAIL`
  (default `admin@localhost.localdomain`) + optional
  `WAKAPI_BOOTSTRAP_ADMIN_PASSWORD`. Create the admin + per-user API keys
  via the Wakapi UI at `http://<host>:5001`.
- **Image pinning:** `WAKAPI_IMAGE` overrides the image
  (default `ghcr.io/muety/wakapi:latest`); pin an explicit tag in
  production.
- DB: SQLite by default (`WAKAPI_DB_TYPE=sqlite3`, `WAKAPI_DB_NAME=/data/wakapi.db`);
  MySQL/Postgres possible per the upstream README if the instance needs it.

## Using it from OlliTeX

1. Log in to Wakapi (admin UI at `http://<host>:5001`); create a user and
   a WakaTime API key per editor user.
2. In OlliTeX: enable the integration (Site settings → WakaTime, the
   `site_settings` wakatime section) and have users link **API URL**
   `http://<host>:5001` + their **API key** (My settings → WakaTime).
3. Security levers (Go web, `problems_30092026C` N1):
   - `WAKATIME_ALLOWED_API_HOSTS` — pin allowed hosts (exact or `*.suffix`);
     unset = any http(s) host (same-host/self-hosted default).
   - `WAKATIME_REJECT_PRIVATE_IPS=1` — resolve + reject private/loopback
     endpoints (public-instance hardening; off by default so same-host
     Wakapi keeps working).
