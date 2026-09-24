# `go/services/gitbridge` — the Git Bridge in Go

The 1:1 Go port of the Node/Java `services/git-bridge` (Overleaf's git
hosting: smart-HTTP `git push`/`fetch`, the write hook, snapshots, gc, and
the web OAuth surface). Entry points:

- `cmd/gitbridge/main.go` (repo root) — the **production proc-receive hook**
  binary (`git_bridge`), i.e. `.git/hooks/proc-receive` for every push; it
  ports Java `WriteLatexPutHook + Bridge`.
- `server/` — the HTTP server layer (smart-protocol receive-pack, OAuth,
  project management, snapshots). Served on the git-bridge port
  (`GIT_BRIDGE_PORT`, 8000 on the e2e stack; host `git-bridge`).

The port strategy splits the Java write path into two processes ("Option 3a"):
(1) a shelled `git receive-pack --stateless-rpc` behind the HTTP layer owns the
smart-HTTP wire and spawns the hook; (2) this Go hook process applies the push.
See `SESSION2_VERIFICATION.md` and the per-subfolder READMEs for the detailed
1:1 mapping.

## Sub-folders (each has its own README)

`bridge` (the push/apply core) · `server` (HTTP layer) · `gitproto` (smart
wire protocol) · `repo` (git CLI runner) · `db` (the DB store + project state) ·
`data` / `filestore` / `snapshot` (data transfer + file store + snapshot API) ·
`gc` (garbage collection jobs) · `resource` (resource cache) · `swap` (S3 swap
store) · `giterrors` (user-facing error types) · `config` (bridge config) ·
`util` (client-IP helper) · `wglog` (log level helper).

Build: `go build ./go/services/gitbridge/... ./cmd/gitbridge`. Tests:
`go test ./go/services/gitbridge/...` (each subpackage is unit-tested against
the ported contracts).
