# `projectlist` — web feature package

`GET /user/projects` (P4.1) — the project list with owner+collaborator best-access dedup (the dedup bug fixed here is pinned by the P4 gates).

`POST /api/project` (P7 U1, `apiproject.go`) — the hub project-list JSON
(filter/sort/page semantics over the same shared query helpers in
`queries.go`; token dedup; archived>trashed; readOnly-token nulls; stable
lastUpdated sort including the mongoose lazy default-fill modeling —
pinned in `apiproject_test.go` + the U1 parity gate).

Read the package doc comments in this folder for the exact Node source mapping and
the response pins. Wired into the binary from `cmd/web/main.go` (repo root);
flipped via `server-ce/nginx/flips/web-p*.conf` and locked by a parity gate
under `tests/e2e/specs/parity/` (see the phase unit in `WEB_GO_PLAN.md`).
