# `authpages` — web feature package

The web authentication surface (P1/P2 wave): login, logout, register, and the password/reset-adjacent pages. Contract pins cited per route against the live Node service (e2e, 2026-09-13); the e2e gate re-proves them on every flip.

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
