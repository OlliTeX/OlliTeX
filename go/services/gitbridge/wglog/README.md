# `gitbridge/wglog`

logging — ports `util/Log.java`: static log helpers honouring `LOG_LEVEL` (the java.util.logging bridge semantics) so bridge logs land with the Node-era levels.

Part of the Go Git Bridge port (see `../README.md`). 1:1 with the corresponding
`services/git-bridge` (Java/Node) piece; unit-tested against the ported contract.
