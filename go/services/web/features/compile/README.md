# `compile` — web feature package

The editor COMPILE control plane (P5.2a): compile trigger/state/log/cancel for a project's compile queue, pinned against the Node compile surface the editor talks to.

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
