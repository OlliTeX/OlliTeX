# `notifications` — web feature package

The notifications module surface (P6.14): `/notifications/preferences` family + `/user/notification-preferences` redirects + `/user/send-test-email` (over the Go notifications service).

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
