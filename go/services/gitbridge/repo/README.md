# `gitbridge/repo`

the git CLI facade — `runGit` shells `git <args>` in a working dir with an optional env, plus the repo-state helpers (refs, HEAD, index plumbing) the bridge needs. Replaces JGit's `org.eclipse.jgit` calls with the `git` binary the deployment already ships.

Part of the Go Git Bridge port (see `../README.md`). 1:1 with the corresponding
`services/git-bridge` (Java/Node) piece; unit-tested against the ported contract.
