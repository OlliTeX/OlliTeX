# `pageshells` — web feature package

The page-shell (PSH) surface (P6.18): services/web/modules/page-shells — the legacy-shell redirect/redirect-family routes (UI-R10 W8).

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
