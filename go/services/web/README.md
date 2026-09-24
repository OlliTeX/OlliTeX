# `go/services/web` — the Go web backend (1:1 for Node `services/web`)

The monolithic web backend, ported incrementally in WEB_GO_PLAN phases P1…P6
(2026-09-13 → 2026-09-21). Every flip is oracle-pinned against the live Node
service and locked by a parity gate under `tests/e2e/specs/parity/`.

## Layout

| path | what it is |
| --- | --- |
| `core/` | the request framework: `App`, `Route`/`Feature` registration, `Cxt`/`Res`, session+CSRF, Mongo lazy client, mail, one-time tokens, ETag/redirect/CSP/headers, rate-limit seam (see `core/README.md`) |
| `features/` | one package per Node route surface (30 packages — index in `features/README.md`) |
| `views/` | the baked-HTML view layer + page renderers (see `views/README.md`) |
| `contract/routes.csv` | the machine-readable Node-router route inventory (160 routes) used as the flip checklist |

Binary: `cmd/web` (repo root) wires `core.App`, registers every feature
(`cmd/web/main.go`), and runs on the Go port (4010 on the e2e stack; the runit
service `web-go-overleaf`). Node runs the same feature set on 4000
(`web-overleaf`); `server-ce/nginx/flips/web-p*.conf` are the per-unit nginx
flips that route specific locations to Go (method-guarded, wrong-method falls
through to Node).

## Conventions (keep when extending)

- **1:1 drop-in**: same route, same status, same body bytes (oracle-pinned),
  same headers (CSP/COOP/permissions/ETag where Node sets them).
- **No new routes**; Go-only needs are added as narrow seams and documented.
- **Oracle discipline**: pins cite the Node source (file:line) or the live
  capture (`tools/webviews-capture*.py`, gate batteries in the parity specs).
- **Green slice**: `go build ./... && go vet ./go/services/web/... && gofmt -l
  <unit dirs>` clean, unit tests green, the flip parity gate green, then
  commit with only the unit staged (never `git add -A`).
