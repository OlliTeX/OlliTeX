# `gitbridge/resource`

the resource cache — ports `bridge/resource/*` (`ResourceCache` + `UrlResourceCache`): caches fetched resources (avatar/logo/etc.) with URL-scoped expiry.

Part of the Go Git Bridge port (see `../README.md`). 1:1 with the corresponding
`services/git-bridge` (Java/Node) piece; unit-tested against the ported contract.
