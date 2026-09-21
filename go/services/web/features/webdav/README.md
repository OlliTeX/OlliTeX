# `webdav` — web feature package

The WebDAV module surface (P6.9): `/user/webdav/*` + `/project/:id/webdav/*` + `/project/new/webdav` — client of the Go webdavinterface service; the connect state machine is pinned in the webdav parity gate.

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
