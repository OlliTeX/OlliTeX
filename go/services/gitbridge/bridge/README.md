# `gitbridge/bridge`

the heart of the Git Bridge — ports `bridge/Bridge.java`. The core apply/pull/push state machine that turns a git push into project-file changes (and back), driving `db`, `data`, `filestore` and the commit-message parsing that feeds the sync engine.

Part of the Go Git Bridge port (see `../README.md`). 1:1 with the corresponding
`services/git-bridge` (Java/Node) piece; unit-tested against the ported contract.
