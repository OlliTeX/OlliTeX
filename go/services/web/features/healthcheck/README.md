# `healthcheck` — web feature package

`/health_check` + `/status` liveness endpoints (HealthCheckController.mjs + /status semantics), pinned live 2026-09-16.

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
