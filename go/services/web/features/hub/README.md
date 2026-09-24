# `hub` — web feature package

The ollitex-hub module server surface (P6.1): the hub API the /hub frontend calls — mostly proxies of already-flipped P3/P4 endpoints + its own HubController leaves.

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
