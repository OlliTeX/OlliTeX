# `gitbridge/data`

Ports the Java `data/` package:
- `data/model/Snapshot` + `snapshot/getforversion` + `getsavedvers` models
  (the saved-snapshot version model the bridge queries),
- `data/CannotAcquireLockException`,
- `data/ProjectLockImpl` — the per-project re-entrant lock + global shutdown
  barrier, exposing the `bridge/lock` `ProjectLock` + `LockGuard` contract,
- `data/LockAllWaiter`.

Java `ReentrantLock` re-entrancy is per-THREAD; Go goroutines have no
identity, so the port makes the lock **holder explicit**: the outer acquisition
returns a `*Holder` token and nested re-entrant acquire/release take that same
token, bumping/debumping its depth. Observable contract unchanged.

Part of the Go Git Bridge port (see `../README.md`); unit-tested 1:1 against
the ported lock + snapshot contracts.
