# D22 (8cbc1526) — OlliTeX observability (Prometheus)

Phase B: a single-host, static-scrape Prometheus sidecar for the Go stack.
No k8s, no service discovery: every OlliTeX Go service exposes `/metrics`
on its internal docker port (D22 A2 wiring — `go/libraries/ometrics/
prometheus.go`), and this sidecar scrapes them straight across the docker
network.

## Start (opt-in profile — the default stack is unchanged)

    cd <overleaf checkout>
    docker compose --profile d22 up -d

Prometheus listens on host port **9090** (for Grafana phase C and operator
tooling), scrapes every 15s, retains 15d, and supports `POST /-/reload`
(`--web.enable-lifecycle`) so phase-D rule files can be dropped in without
a container cycle.

## What gets scraped (internal ports — pinned by the D22 A2 wiring)

| job                     | target                 | service           |
|-------------------------|------------------------|-------------------|
| `ollitex-web`           | `sharelatex:4000`      | the Go web        |
| `ollitex-collab`        | `sharelatex:3450`      | ygo collab engine |
| `ollitex-realtime`      | `sharelatex:3026`      | socket.io bus     |
| `ollitex-chat`          | `sharelatex:3010`      | chat              |
| `ollitex-docstore`      | `sharelatex:3016`      | doc store         |
| `ollitex-filestore`     | `sharelatex:3009`      | file store        |
| `ollitex-notifications` | `sharelatex:3042`      | notifications     |
| `ollitex-history-v1`    | `sharelatex:3100`      | OT legacy plane   |
| `ollitex-project-history` | `sharelatex:3054`    | OT legacy plane   |
| `ollitex-git-bridge`    | `git-bridge:8000`      | git bridge        |
| `node`                  | `node-exporter:9100`   | host CPU/RAM/disk |

The `sharelatex` / `git-bridge` host names are placeholders resolved at
container start from the compose env (`OVERLEAF_HOST` / `GITBRIDGE_HOST`)
— if you rename the server container in a custom compose, set
`OVERLEAF_HOST` accordingly.

## Security posture (by design)

- `/metrics` is mounted on the service **internal** ports only. The services
  bind docker-internal listeners and **the HAProxy edge never forwards
  /metrics** — exposing it publicly is an explicit owner decision, not a
  default.
- Metrics carry low-cardinality labels only: the `path` label collapses
  document/project/user ids to placeholders (`ollitex` cardinality contract
  in `routeLabel`).

## Series naming (Node-faithful)

Request metrics keep the node-`@overleaf/metrics` names so panels written
against the old registry keep working: `requests_total{method,path,code}`
semantics via the `requests` counter and the `request_time` summary
(quantiles p1…p999). Default labels: `app` (= `METRICS_APP_NAME` per
service, set by the runit units) and `host` (container hostname).

## Phases (8cbc1526)

- **A — instrument** ✅ A1 encoder/handler/middleware (`f478d241bf`) + A2 all
  ten services wired (`417351bca7`)
- **B — sidecar** ✅ this file + the compose profile (11 static jobs +
  node-exporter, retention 15d)
- **C — Grafana** ✅ `build-images/grafana/` (datasource + dashboard
  provisioning, same `d22` profile; see that README). **Owned decision
  pending:** the optional kiosk iframe on `/hub` (a product-surface change
  — not taken by default).
- **D — alerts** ✅ `rules/ollitex.rules.yml` (5 rules, promtool-validated) +
  `build-images/alertmanager/` (sidecar, the webhook bridge) → Go web
  `POST /internal/alerts` (instancestats feature) → one mail per firing
  alert to the instance-stats alert configuration, same SMTP pipeline as
  the `/admin` test button. Recipients: the
  `instanceStatAlertConfigs` singleton (set in the /admin site-settings
  API).
- **F — legacy**: after C/D are stable in the image, the Node-era metrics
  series that have no Go source can be retired from any operator dashboards
