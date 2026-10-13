# build-images/ JUNK AUDIT — 2026-10-13 (owner work order)

Questions answered: (1) is there junk in `build-images/` that can be retired?
(2) do any of its files actually belong in `images/main-amd64/`?

## Role separation (evidence)
- `images/main-amd64/` = **app image build context**: Dockerfile, config/, cron/,
  runit/, init_scripts/, nginx/, static/, genScript.js, grunt/ (3.9M static assets).
- `build-images/` = **build-support / monitoring / legacy test infra**: alertmanager,
  grafana, prometheus, edge (haproxy), config (legacy Node env mappings), bin
  migration scripts, scripts (credential rotation), test/ (legacy e2e image suite),
  e2e (notifications smoke), cloudbuild config, WAKAPI.md.
- **Overlap check (md5 identity scan): ZERO byte-identical files between the two
  trees** → nothing in build-images is a duplicate of main-amd64 content.
- **Reference scan:** `Makefile` already merged build-images' build targets
  (2026-10-01); `develop/clsi-nginx.conf` says "keep in sync with
  build-images/nginx/clsi-nginx.conf" — **that path does not exist** (stale ref;
  the develop copy is the live one). `images/main-amd64/init_scripts/000_check_*`
  mentions `build-images/bin/rename-env-vars-5-0.sh` only inside user-facing error
  hint text, not as a real dependency. CI (`e2e.yml:76`) uses `working-directory:
  build-images` for one step (keep — not junk).

## Junk candidates (retire behind owner deletion gate — none removed by me)
| Path | Size | Evidence |
|---|---|---|
| `build-images/test/` | 280K | Legacy Cypress+Playwright image test suite (docker-compose + specs + Dockerfile). No CI reference except its own compose; superseded by `tests/e2e` (124 specs, the CI e2e job). `tests/tools/sso-test` keeps an external copy of what it needs. |
| `build-images/eslint.config.mjs` | 4K | Cypress-plugin eslint config — orphaned with the test/ suite above. |
| `build-images/bin/rename-env-vars-5-0.sh` | 4K | 5.0-era env migration one-off; only referenced inside error-hint strings. |
| `build-images/bin/import_pr_patch.sh` | 4K | One-off PR patch importer; no consumers found. |
| `build-images/lib/.dropbox-secrets` | 30B | Tracked, NOT gitignored; content = `# Dropbox OAuth configuration` comment placeholder (not a live secret); **zero consumers** repo-wide. Name invites leak-sweep false alarms — retire or rename to a README note. |
| `build-images/e2e/notifications-smoke.sh` | 8K | Standalone smoke script; functionally covered by `notifications`/`notifications-full` arcs. Consumer = only itself (header usage line). |
| `build-images/cloudbuild.public.yaml` | 4K | gCloud builder config; no references in Makefile/CI; only meaningful if publishing to GCR (owner process call). |
| `build-images/config/custom-environment-variables.json` | 4K | Legacy Node image env mapping; referenced only as a DOCUMENTATION comment in `go/services/historyv1` (env names kept 1:1). Keep-as-doc or fold the comment in and retire. |

## Keep (live, referenced)
- `grafana/` (provisioning + dashboards — the admin Grafana bridge arc depends on it),
  `prometheus/`, `alertmanager/` (monitoring plane, baked in),
  `edge/` (haproxy + compose for the edge bake),
- `bin/shared/`, `scripts/rotate-sync-credential-keys.mjs` (credential-rotation ops),
- `WAKAPI.md` (wakapi pin), `test/`-adjacent `gitbridge-runtime.json` if the legacy
  suite is retired (move to `tests/tools` reference first),
- `e2e.yml` working-directory target (verify which step before retiring test/).

## main-amd64 side (no migration needed)
- No build-images file is missing from / belongs in `images/main-amd64`: the two
  trees serve disjoint roles and share zero content. The ONE cross-tree staleness is
  the `develop/clsi-nginx.conf` header comment (points at a non-existent
  `build-images/nginx/` — fix the comment or relocate, owner call).
- `images/main-amd64/grunt/` — legacy Node build helper inside the app context;
  candidate for a future server-side sweep only if the Node frontend retire
  lands (out of scope here).

## Recommendation (owner gate)
1. Retire the 8 candidates above (comment-first per owner deletion-gate rule, then delete).
2. Keep monitoring/edge/bin-shared/scripts/refs intact.
3. I can execute the comment-gate pass on an owner GO.
