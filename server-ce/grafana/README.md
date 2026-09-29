# D22 (8cbc1526) — phase C: Grafana

Single-host, file-provisioned Grafana OSS for the OlliTeX metrics sidecar.
Runs in the same opt-in `d22` compose profile as Prometheus and
node-exporter; the default stack is unchanged.

## Start

    cd <overleaf checkout>
    docker compose --profile d22 up -d

Grafana listens on host port **3180** → container :3000 (avoids the
internal `:3000` web-api port used by the app containers). Default admin
credentials: `admin` / `ollitex-observability` (override with
`GRAFANA_ADMIN_PASSWORD` in the compose environment). Sign-ups are off,
telemetry is off.

## Provisioning (no click-ops)

- **Datasource** — `provisioning/datasources/prometheus.yml`:
  `prometheus:9090`, uid `ollitex-prometheus`, default, not editable via
  UI. Query all panels with this uid and the URL is portable.
- **Dashboards** — `provisioning/dashboards/ollitex.yml` (file provider,
  folder `OlliTeX`) loading `dashboards/ollitex-overview.json`:
  - HTTP request rate (summed, per-`app`)
  - 5xx share per app
  - p99 request time per app
  - RSS memory per app
  - node: CPU / memory / root-disk / uptime

  All queries use the Node-faithful series pinned by D22 A1/A2
  (`requests{method,path,code}`, `request_time{quantile}` summary,
  `process_resident_memory_bytes`, node-exporter classics).

## Kiosk on /hub (owned decision — intentionally NOT taken by default)

Embedding the dashboard as an iframe surface on `/hub` changes a product
surface (an authz-adjacent view of internal metrics) — flagged to the
owner instead of building it. The compose service, provisioning and
dashboard are the default; the kiosk remains an explicit opt-in.

## Operational notes

- The dashboard data volume persists at `~/grafana_data` (compose mount).
- To reload dashboards after an edit: the file provider re-reads on
  container restart (`docker compose --profile d22 restart grafana`);
  datasources reload via the Grafana API if needed.
- This is an operator tool surface: it is **not** part of the app, not
  reachable through the HAProxy edge, and not covered by the e2e battery.
