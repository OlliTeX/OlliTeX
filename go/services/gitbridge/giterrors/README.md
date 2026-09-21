# `gitbridge/giterrors`

the user-facing error hierarchy — ports the git-bridge exception types (auth, not-found, conflict, rate-limit) and their HTTP mapping so Go returns the same codes + bodies the Node/Java bridge did.

Part of the Go Git Bridge port (see `../README.md`). 1:1 with the corresponding
`services/git-bridge` (Java/Node) piece; unit-tested against the ported contract.
