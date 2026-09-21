# `gitbridge/gc`

garbage collection — ports `bridge/gc` (`GcJob` + `GcJobImpl`): schedules and drives the repo housekeeping (prune, repack, expire) the bridge runs against its git directories.

Part of the Go Git Bridge port (see `../README.md`). 1:1 with the corresponding
`services/git-bridge` (Java/Node) piece; unit-tested against the ported contract.
