# `templates` — web feature package

The template-gallery surface (P6.13, services/web/modules/template-gallery), oracle-pinned 2026-09-18 on the e2e stack.

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
