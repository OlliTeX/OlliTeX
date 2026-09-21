# `adminusers` — web feature package

The admin user-management surface (P3.x): `POST /admin/user/create` + the admin user list/set routes. Node source: the admin-tools user surface under services/web (AdminUsersController family). Admin-gated; contract pinned by the P3.x parity gate.

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
