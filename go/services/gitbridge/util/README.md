# `gitbridge/util`

small helpers — ports `Util.getClientIp` (first `X-Forwarded-For` value, else remote addr) + the other shared Java `Util` routines the bridge calls.

Part of the Go Git Bridge port (see `../README.md`). 1:1 with the corresponding
`services/git-bridge` (Java/Node) piece; unit-tested against the ported contract.
