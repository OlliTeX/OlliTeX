# `gitbridge` — web feature package

The web-side Git Bridge surface (P6.x): the OAuth/token/project/blob routes the web app serves against the Go gitbridge service (port `GIT_BRIDGE_PORT`).

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
