# The OlliTeX Toolkit over SSH (installer & operator console)

Goal: install the OlliTeX stack on a plain Docker host with **one SSH login
and no other tooling**, then drive the whole instance — stack lifecycle,
settings, logs, shells, doctor, backup — from the toolkit TUI.

## 1. What it is

The toolkit is a single static Go binary (`ollitex/toolkit-tui`, alpine
3.24 base) that:

1. **owns the stack lifecycle** — it renders a Docker Compose plan from the
   config store and `up`/`down` the overleaf stack through the host Docker
   socket;
2. **is the admin console** — a Bubble Tea TUI served over SSH
   (charmbracelet/wish) on port **2222**: stack status, settings, logs,
   interactive shells, doctor, backup, n-gram model downloads, TLS import;
3. **is the config CLI** — the same binary image ships `configdb`, the
   command-line face of the config store (`list` / `get` / `set` /
   `import-env` / `init`), so every screen you can drive, you can also script.

**Host contract (owner directive):** the host only needs

| # | Requirement | How it is used |
|---|-------------|----------------|
| 1 | Docker (a daemon reachable at a socket) | the socket is mounted into the toolkit container; orchestration never needs host-side docker |
| 2 | **One mounted local folder** (the data dir) | SSH host key, config-store backups, journal, the rendered env file, the stack's data dirs — all under this one folder |
| 3 | A reachable config store (Postgres DSN) | **the single source of truth** for every setting; no docker-env settings plane, no rc files, no fallbacks |

Nothing else: no Node, no shell scripts, no env files to edit by hand.

## 2. Build

```bash
make build-toolkit            # → ollitex/toolkit-tui:main
# (defaults: golang musl builder, alpine:3.24 runtime; both binaries
#  `toolkit` + `configdb` are static — they run anywhere alpine runs)
```

## 3. Deploy

```bash
ssh-keygen -f /opt/ollitex/data/ssh_host_key -t ed25519 -N '' -q   # the one folder, pre-seeded
# or let the toolkit generate it on first boot (it does, 0600)

docker run -d --name ollitex-toolkit \
  --restart unless-stopped \
  -v /opt/ollitex/data:/opt/ollitex/data \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -e CONFIG_DB_DSN='postgres://overleaf:CHANGE_ME@db-host:5432/overleaf-config?sslmode=disable' \
  -e OLLITEX_TOOLKIT_PASSWORD_FILE=/opt/ollitex/data/toolkit_ssh_password \
  -p 127.0.0.1:2222:2222 \
  ollitex/toolkit-tui:main serve
```

- `-e CONFIG_DB_DSN` — the single source of truth DSN (or `DATABASE_URL` /
  `HISTORY_CONNECTION_STRING`; the chain is probed in that order).
- `OLLITEX_TOOLKIT_PASSWORD` **or** `OLLITEX_TOOLKIT_PASSWORD_FILE` —
  exactly one (the file variant keeps the secret out of the docker env).
  Never put a literal credential in a compose file or a shell history entry.
- The SSH user is `ollitex` (in-process auth — no host account is created).

**The data dir layout** (all real folder names — backups are obvious from
the layout, no mystery volumes):

```
data/
├── ssh_host_key              # the SSH host key (0600)
├── config.db.bak-<ts>.json   # `backup` snapshots (dump + restore)
├── toolkit.env               # rendered compose env plane (auditable)
├── config/                   # operator overrides (see §7.4) + TLS material
├── mongo/                   # MongoDB data (owner pin: mongo:9.0)
├── redis/                   # Redis data (owner pin: redis:8.10-alpine3.23)
├── postgres/                # Postgres data (config DB + history planes)
├── seaweedfs/               # SeaweedFS S3 state (default filestore/docstore backend)
├── gitbridge/               # Git bridge data
├── languagetool/            # n-gram models (+ ngrams/ read-only mount)
└── monitoring/              # Prometheus TSDB + Grafana state + dashboards (opt-in)
```

## 4. Sign in

```
$ ssh -p 2222 ollitex@your-host
ollitex@your-host's password:        ← the OLLITEX_TOOLKIT_PASSWORD value
OlliTeX Toolkit 0.1.0 (rev 74da2e6)
```

![toolkit TUI dashboard](../assets/installation/05-toolkit-1.png)

**The layout** — the classic console structure (midnight-commander /
freebsd-installer direction): the **menu bar** on top (`File · Stack ·
Settings · Shells · Doctor · Backup · Actions · Help` — `F10` or a click on a
label opens it), the **two bordered panes** below (left = the master list,
right = the detail of the selection), the **status line** (stack/store/job
state) and the **keystrip** (`[j/k] move [enter] open [u] start [d] stop
…`). Destructive moves (stop · restart · restore · quit) open a **mc prompt
box** — a bordered `yes / no` dialog centered over the panes (a stray click is
a NO). The master list:

| # | Item | What it does |
|---|------|--------------|
| 1 | **Dashboard** `N/M up` | the overview (stack + store + host contract) |
| 2 | **Stack** `N/M up` | the compose plan + per-container status; start (`u`) / stop (`d`) / restart (`r`) / pull (`p`) — the action row is clickable too |
| 3 | **Logs** | tail any container's logs (`h`/`l` cycle, `f` refresh) |
| 4 | **Settings** | the config store, edit-in-place (secret keys masked; stored values never re-shown) |
| 5 | **Shells** | live `exec` into: `mongo (mongosh)` → sharelatex · `postgres (psql)` → overleaf-history-v1 · `app sh (/app)` · `app sh (/home/overleaf)` — `1`–`4` |
| 6 | **Actions** | TLS cert/key import (`t`, 2-step) · n-gram downloads (`n`) · n-gram status (`l`) · first-admin bootstrap (`b`) |
| 7 | **Doctor** | docker / store / compose / data-dir / stack health rows (green/red; `r` re-runs) |
| 8 | **Backup** | dump (`b`) / restore (`r`, confirmed) the config store into the data dir |
| 9 | **About** | build revision + the config-plane provenance |

Global keys: `F10` menu bar · `F1`/`?` help · `s` start · `t` (or `d`) stop
(confirmed) · `j`/`k` + `1`–`8` the master list · `enter` open · `ctrl-c` →
shell SIGINT · `ctrl-z` → detach the shell back to the panes · in the shell,
`exit` returns to the TUI.

The TUI runtime is the retained-mode stack `rivo/tview` + `gdamore/tcell`
(ported 2026-10-07, owner item AI: tview repaints only changed cells over the
SSH link, which is the fix for the previous full-frame redraw slowness).
Over SSH the TUI runs on a screen bound to the session's io (the server side
is not a `/dev/tty` — see the `SessionScreen` helper in `tui.go`); locally
(`toolkit local` / `toolkit tui`) it runs on the terminal's own screen.
The classic console look (menu strip · two panes · status + keystrip footer ·
centered confirm/prompt boxes · the j/k + number key map) and the
charmbracelet helper libraries it originally used are credited in
`CREDITS.md` per the project's credits policy.

## 5. First boot (the happy path)

1. **Seed the store** — `toolkit init` (or the TUI) creates the config DB
   table, field-encrypts the key slot, and seeds every registry key at its
   default (`configdb list --all` shows the full map with groups/kinds).
   Existing values are never clobbered.

   ![configdb surface](../assets/installation/05-toolkit-3.png)

2. **Check the plan** — `plan` is the read-only answer to "what would
   `up` do?": the overlay list (merge order), the rendered env file path,
   provenance notes (the generated secrets — `POSTGRES_PASSWORD`,
   the six app-env secrets, `GRAFANA_ADMIN_PASSWORD` — are **noted but
   never printed**), and a **daemon-valid gate** (`docker compose config
   --quiet`) — you get compose's own verdict before anything starts.

   ![toolkit plan](../assets/installation/05-toolkit-2.png)

3. **Start** — `s` on the dashboard (or `stack up` from the Stack screen).
   The toolkit renders the env plane into `data/toolkit.env` (the six
   app-env secrets + DSN + generated material, masked), materializes the
   optional services' files, and brings the stack up in the right order.
   The app container passes its bootstrap gate (`All checks passed`), runs
   its migrations, and turns `healthy`. Verified: 9/9 containers healthy
   and the app answering `GET /` → 302 `/login` (the Go service's
   anonymous bounce).

   ![stack healthy](../assets/installation/05-toolkit-4.png)

## 6. The stack (compose overlays from the store)

Each optional service is one overlay, toggled by store keys (defaults in
parentheses):

| Service | Toggle key (default) | Data dir | Notes |
|---------|---------------------|----------|-------|
| overleaf app (`ollitex`) | — (always) | `data/overleaf` | the app image (`IMAGE` key); host port `OVERLEAF_PORT` (80) |
| MongoDB | `MONGO_ENABLED` (true) | `data/mongo` | **owner pin mongo:9.0**; `mongosh` shell |
| Redis | `REDIS_ENABLED` (true) | `data/redis` | **owner pin redis:8.10-alpine3.23**; AOF persistence |
| Postgres | `POSTGRES_ENABLED` (true) | `data/postgres` | config DB + history planes; generated `POSTGRES_PASSWORD` if unset |
| SeaweedFS (S3) | `SEAWEEDFS_ENABLED` (true) | `data/seaweedfs` | 4 containers (master/volume/filer/s3) — the durable filestore/docstore backend |
| Git bridge | `GITBRIDGE_SERVICE_ENABLED` (true) | `data/gitbridge` | P7 cutover: the Go git-bridge between app and GitHub |
| checkuser | `CHECKUSER_ENABLED` (false) | `data/checkuser` | sandboxed-compiles user check |
| nginx/TLS | `NGINX_ENABLED` (false) | `config/` (certs) | TLS proxy; ports 80/443 |
| LanguageTool | `LANGUAGE_TOOL_ENABLED` (false) | `data/languagetool` | grammar checking + n-gram downloads via **Actions** |
| **Monitoring (D22)** | `MONITORING_ENABLED` (false) | `data/monitoring` | see §7 |

Every TUI-managed container carries a **docker healthcheck** (owner
policy) — the Stack view and `doctor` read those states directly.

## 7. Monitoring (Prometheus + Grafana, opt-in)

`MONITORING_ENABLED=true` (then `up` again) adds the D22 observability
ecosystem, all bind-mounted under `data/monitoring/`:

| Container | Image (pin) | Healthcheck | Purpose |
|-----------|-------------|-------------|---------|
| `monitoring-prometheus` | `prom/prometheus:v2.53.5` | `/-healthy` | scrapes every Go service (internal ports) + the exporters + node |
| `monitoring-mongodb-exporter` | `percona/mongodb_exporter:0.43.0` | binary startability | Mongo → Prometheus (the `prom/` mirror was retired upstream) |
| `monitoring-redis-exporter` | `oliver006/redis_exporter:v1.58.0-alpine` | `/metrics` **and** a TCP probe of the redis server | Redis → Prometheus |
| `monitoring-mongo-probe` | `mongo:9.0` (the owner pin) | a real `mongosh ping` (protocol-level) | DB liveness even though the exporter image ships no shell |
| `monitoring-node-exporter` | `prom/node-exporter:v1.9.1` | `/metrics` | host CPU/mem/disk |
| `monitoring-grafana` | `grafana/grafana-oss:11.6.0` | `/api/health` | dashboards: **OlliTeX — overview** (HTTP/5xx/p99/RSS + node) and **MongoDB + Redis** overview; user `admin` |

- **Ports** (bound to `OVERLEAF_LISTEN_IP`): Prometheus **9090**, Grafana
  **3180** (deliberately not :3000), exporters 9216/9121.
- **Grafana admin password** — generated on first enable and **stored** in
  the config store (never printed by the plan); retrieve it with
  `configdb get GRAFANA_ADMIN_PASSWORD --reveal`.
- **Scrape targets** follow the compose container names; if an operator
  override renamed a database container (§7.4), set
  `MONITORING_MONGO_HOST` / `MONITORING_REDIS_HOST` (defaults
  `overleafmongo` / `overleafredis`) so the exporters + probe point at the
  right names.
- The scrape config is rendered into the data dir at plan time
  (`data/monitoring/prometheus.yml`) — inspectable, and the placeholders
  are provably gone (unit-pinned).

### Live dashboards pane in /hub (opt-in, off by default)

The hub's **Site settings → General → Instance statistics** section can
embed the Grafana dashboards as a kiosk iframe ("Live dashboards"). The
embed is an explicit opt-in **all the way down** — with Grafana still
requiring admin auth, a cross-origin iframe of it is a credential-phishing
surface, so the default state is: pane hidden, anonymous access disabled,
CSP frame-ancestors `self`. To enable:

1. **Point the app at Grafana** — set the store key `HUB_GRAFANA_EMBED_URL`
   (e.g. `http://<grafana-host>:3180`); the toolkit env plane renders it
   into the app, and the hub pane's endpoint resolves it first (an env value
   is conclusive — it wins over the site_settings section of the same name).
   The admin can alternatively set `Site settings → monitoring →
   grafanaEmbedURL` at runtime (that section is the runtime-managed source).
2. **Allow cross-origin framing** on Grafana — `GRAFANA_ANONYMOUS=true`
   (rendered to `GF_AUTH_ANONYMOUS_ENABLED` + `Viewer` org role) and
   `GRAFANA_CSP_FRAME_ANCESTORS=<the hub origin>` (never `*`).

The pane then switches between the two shipped dashboards
(`ollitex-overview`, `ollitex-toolkit-mongo-redis`) and offers both the
kiosk (`{base}/kiosk-d/{uid}`) and full (`{base}/d/{uid}`) URLs. The pane
endpoint is site-admin only and returns the dashboards with their
kiosk/full URLs — verified live against a disposable web instance on the
e2e stack: store path, env-conclusive override (trailing slash trimmed),
and the off-by-default `{enabled:false}` state all behave as pinned.

## 8. Operations

### 8.1 doctor & health

`doctor` runs one-shot health rows (docker reachable, store reachable,
compose plan valid, container states); `toolkit health` is the same for
cron (exit code 0/1). `autofix --once` (or `--interval`) is the autoheal
pass: restart `unhealthy` containers **with a per-container cooldown** so a
broken service can't tight-restart-loop.

### 8.2 logs & shells

**Logs** tails any container. **Shells** (`exec`, bidirectional) covers the
three daily drivers: `mongosh sharelatex`, `psql` into the history DB, and
the app's `/app` — everything an operator needs without touching the host.

### 8.3 backups

**Backup** dumps the config store (including field-encrypted secrets) into
the data dir as a timestamped file; restore is the same screen. The data
dirs themselves are ordinary folders — the host's normal backup tooling
covers them, and the layout (§3) makes that obvious.

### 8.4 operator overrides (documented escape hatch)

A file at `data/config/docker-compose.override.yml` is appended **last** in
the compose merge (highest precedence) — the toolkit reports
`operator override active: …` in the plan. Use it to rename a container
(name collision with another instance on a shared host), add an env var to
one service, or pin anything the store doesn't model. The toolkit's
generated plan still validates the merged result with the daemon before
`up` — an invalid override fails **before** anything starts.

## 9. Safety & secrets

- **One true source**: settings live in the config store (Postgres). There
  is no docker-env settings plane and no rc-file fallback — if the TUI and
  `configdb` disagree, the store wins.
- **App-env contract**: the six secrets the app bootstrap hard-requires or
  the Go plane needs (`OVERLEAF_INVITE_TOKEN_SECRET`,
  `OVERLEAF_SESSION_SECRET`, `CRYPTO_RANDOM`, `WEB_API_PASSWORD`,
  `SHARED_SERVICE_TOKEN`, `OT_JWT_AUTH_KEY`) plus the Mongo DSN pair
  (`MONGO_URL` / `OVERLEAF_MONGO_URL`) live in the store. Unset secrets
  are generated + stored at plan time (provenance noted, never printed);
  an operator-set value always wins.
- **Secrets** (DB passwords, tokens, TLS material, the Grafana admin
  password) are stored field-encrypted and masked in listings; the TUI
  never prints them; `plan` notes *that one was generated*, not the value.
- **Offline emergency path**: `CONFIG_DB_PATH=<file>` is an **explicit**
  SQLite mode (loud, documented, test-backed) for when Postgres itself is
  unreachable — never an implicit fallback (a configured PG that is down is
  a hard error, by design).
- **Retraction guard**: a retracted `IMAGE_VERSION` (e.g. 5.0.1) fails the
  plan unless `OVERLEAF_SKIP_RETRACTION_CHECK` is set — air-gap override,
  explicit.
- **CGO/static rule**: every shipped binary is statically linked
  (`CGO_ENABLED=0` or musl-static) so it runs on hosts without a C runtime
  loader.

## 10. Troubleshooting

| Symptom | First check |
|---------|-------------|
| `up` fails on a container **name** collision | another instance on the host already uses that name → rename via the §8.4 override + set the `MONITORING_*_HOST` knobs if monitoring targets the renamed DB |
| Grafana login 401 right after a restore | a stale SQLite WAL can resurrect old user rows — wipe the (non-secret) `data/monitoring/grafana-data` and let Grafana re-seed from `GF_SECURITY_ADMIN_PASSWORD` |
| the /hub "Live dashboards" pane is hidden | opt-in by design: set `HUB_GRAFANA_EMBED_URL` (store key → app env) or the `monitoring.grafanaEmbedURL` site-settings section — the endpoint returns `{enabled:false}` until then (the admin-gated pane is off unless explicitly enabled) |
| exporter `unhealthy` but the DB is fine | check `MONITORING_MONGO_HOST` / `MONITORING_REDIS_HOST` point at the live container names |
| `ollitex-*` scrape targets stay DOWN while mongodb/redis/node are UP | by image design the Go app services bind `127.0.0.1` inside the ollitex container (the in-container nginx fronts them) — cross-container scrapes can't reach loopback. The scrape config already lists them and lights up automatically when a deployment exposes them on the compose network; the DB/exporter path is the fully-proven path in the multi-container layout |
| `plan` fails with `compose config FAILED` | read the daemon's message — that is the exact cause (this gate is there to fail early) |
| app container stuck before `healthy` | the app-env contract (six secrets + MONGO_URL/OVERLEAF_MONGO_URL) is rendered from the store and validated by the plan; a crash-loop with `MongoTopologyClosedError` = the DSN pointed at a dead host (the default `mongodb://dockerhost/…` was the live smoke-catch; the store now carries a real DSN — e2e-proven single-node replica set, overridable) |
| SSH refused on :2222 | host firewall / `-p` binding; password = `OLLITEX_TOOLKIT_PASSWORD(_FILE)` |

## 11. Legacy scripts → toolkit map (retirement)

The old `toolkit/bin` scripts are absorbed one-for-one — keep your muscle
memory, aim it at the TUI/CLI:

| old command | toolkit equivalent |
|-------------|--------------------|
| `bin/up`, `bin/down` | dashboard **Stack** → `s` / `t` |
| `bin/doctor` | **Doctor** / `toolkit doctor` |
| `bin/config …` | **Settings** / `configdb …` |
| `bin/logs`, `bin/error-logs` | **Logs** |
| `bin/shell mongo\|pg\|app` | **Shells** |
| `bin/languagetool-ngrams` | **Actions** → n-gram models |
| `bin/backup-config` | **Backup** |
| `bin/images` | **About** + `plan` (image pins are store keys now) |

---

Verified against: OlliTeX @ `74da2e682a` (2026-10-05) — TUI surface from
`go/services/toolkit/ui.go` (`dashItems`), overlay table from
`toolkit/lib/docker-compose.*.yml`, monitoring section from
`toolkit/lib/docker-compose.monitoring.yml`, deploy contract from
`images/toolkit-amd64/Dockerfile`. Terminal captures are fixture-only
(no credentials, no API keys, no real hostnames beyond `example` names);
the `plan`/`configdb`/`stack` captures reflect the 2026-10-05 live smoke
on a disposable stack with generated values masked.
