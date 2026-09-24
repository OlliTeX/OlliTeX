# `gitbridge/gitproto`

implements the git “smart” wire protocol (stateless-RPC upload-pack / receive-pack packets, refs advertisement, pkt-line framing) — the Go equivalent of the Java JGit smart-transport pieces the bridge relies on.

Part of the Go Git Bridge port (see `../README.md`). 1:1 with the corresponding
`services/git-bridge` (Java/Node) piece; unit-tested against the ported contract.
