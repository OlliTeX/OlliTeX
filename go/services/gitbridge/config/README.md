# `gitbridge/config`

bridge configuration — ports `application/config/Config.java` + its nested types: the typed config the bridge reads (paths, swap store, oauth, feature flags) with the same defaults.

Part of the Go Git Bridge port (see `../README.md`). 1:1 with the corresponding
`services/git-bridge` (Java/Node) piece; unit-tested against the ported contract.
