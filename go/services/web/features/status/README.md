# `status` — web feature package

The `/status` endpoints (router.mjs public/privateApiRouter — the plainTextResponse family). Pinned live: 200 text/plain; charset=utf-8 + the X-COOP baseline headers.

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
