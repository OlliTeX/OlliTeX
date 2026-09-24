# `devcsrf` — web feature package

`GET /dev/csrf` (router.mjs:1239) — the dev-only CSRF token helper used by the parity harnesses to mint a valid token for a session.

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
