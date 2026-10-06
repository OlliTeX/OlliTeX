# RETIRED

Dead / historical material moved here (git history retains full provenance).
Kept on disk as evidence; nothing consumes these paths.

## 2026-10-06 audit (owner directive "can this be retired?")

- **tools-capture-p3c-raw/** (was `tools/capture-p3c-raw/`) — raw P3C probe
  captures (settle_clean.html / sessions_*.html / *.json). Zero references
  anywhere in live code, tests, or docs; the live P620 probes keep their own
  oracle captures. Pure historical probe output.
- **tools-service-parity/** (was `tools/service-parity/`) — node-web vs go-web
  env diff probes (diff.mjs / env-diff.mjs + allowlist). Only self-references
  and mentions in the historical GO_CUTOVER_PLAN/WEB_GO_PLAN docs; superseded
  by the tests/e2e parity suites.

## Kept (audit found live consumers — do NOT move)

- **`dockerfiles/cypress/`** — build context of `build-images/test/docker-compose.yml`
  (and `.native.yml`) for the e2e cypress image, driven by `tests/e2e/stack-up.sh`.
- **`tools/capture-p620-raw/`** — oracle captures for the launchpad P620 probes:
  referenced by `go/services/web/views/pages_data_p620.go` ("oracle captures
  (tools/capture-p620-raw/). Do not edit.") and the launchpad README.
