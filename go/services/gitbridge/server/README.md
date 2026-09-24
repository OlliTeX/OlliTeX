# `gitbridge/server`

the HTTP server layer — ports `GitBridgeServer` + all handlers: smart-HTTP receive-pack (shelled `git receive-pack --stateless-rpc`), OAuth token exchange, project management, snapshots and the web-facing endpoints. Served on the git-bridge port (`GIT_BRIDGE_PORT`).

Part of the Go Git Bridge port (see `../README.md`). 1:1 with the corresponding
`services/git-bridge` (Java/Node) piece; unit-tested against the ported contract.
