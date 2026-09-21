# `gitbridge/snapshot`

Ports `bridge/snapshot`:
- `SnapshotApi` interface + `SnapshotApiFacade` (`facade.go`) — satisfied by
  `*NetSnapshotApi` via method promotion; the HTTP-facing snapshot API
  (`/project/:project_id/snapshots`, fetch/create/delete, `getforversion` /
  `getsavedvers`),
- `PostbackManager` + `PostbackPromise` (`postback.go`) — the Java
  `snapshot/push` postback promise chain (snapshot-ready signalling).

Part of the Go Git Bridge port (see `../README.md`); unit-tested
(`snapshot_test.go`).
