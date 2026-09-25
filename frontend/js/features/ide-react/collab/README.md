# ide-react/collab — Yjs client engine (D19/D20/D24)

The document-collaboration client that replaces the retired 2014 OT stack
(ShareJsDoc + SocketIo + OT `realtime.ts`). Server counterpart: the Go
`collab` service (`go/services/collab`, ygo/Yjs-compatible over WS).

## Contract (do not break one-sided)

| Wire element      | Server (Go)                                                                                                           | Client (this package)                                                                   |
| ----------------- | --------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| WS path           | `/collab/{projectId}` (`cmd/collab`)                                                                                  | `collabEndpoint()` → `providers.ts`                                                     |
| Content type name | `collab.TextType = "content"` (`roomdoc.go`; pinned by `roomdoc_test.go::TestTextTypeContract`)                       | `TEXT_TYPE = "content"` (`text-type.ts`; pinned by `test/text-type.test.ts`)            |
| Seeding           | `OnLoadDocument` + `SeedFn` — server is the **single source** of first content (rooms start empty on the client; D19) | engine joins EMPTY; seed arrives via initial sync (pinned by `test/sync.test.ts::seed`) |
| History REST      | `GET/POST /project/:pid/collab/history…` (`features/collabhistory`)                                                   | consumed by the editor UI (S4 wiring)                                                   |
| Auth              | session cookie, same-origin                                                                                           | browser sends cookie with the WS handshake (`providers.ts`, no query params)            |

## Files

- `text-type.ts` — the shared Y.Text type name (wire contract).
- `ydoc.ts` — `newYContent()`: Y.Doc + `content` Y.Text + programmatic
  UndoManager (tracks LOCAL-origin transactions only — remote updates stay
  out of the programmatic stack by construction).
- `sync.ts` — `YTextSync`: the D24 bridge (see below). Core is
  CodeMirror-free and Node-testable.
- `providers.ts` — `collabEndpoint` / `wsBaseUrl` (pure) +
  `attachProviders` (y-websocket → `/collab/:pid`; **y-indexeddb v9**
  offline persistence `IndexeddbPersistence`; browser-gated — Node gets
  `{ws:null,idb:null}` and the engine stays headless-testable).
- `codemirror.ts` — `syncExtension`: the CM6 `updateListener` half
  (spans of `update.changes` in old-space coordinates → `applyLocal`).
- `engine.ts` — `createEngine(projectId, sink, opts)`: the facade.
- `index.ts` — public exports.
- `test/` — Node suites (run with `yarn vitest run` in this workspace):
  convergence, concurrency (disjoint + whole-doc), no-op idempotency,
  delete-to-empty, seed path, undo seam, endpoint/URL contract.

## D24 — the sync design (why it looks the way it does)

- **Local direction = granular delta ops.** The host hands over its edit
  spans (old-space coordinates, exactly what CodeMirror's `iterChanges`
  yields); `applyLocal` replays them onto the Y.Text **right-to-left**,
  delete-then-insert per span, in ONE transaction (undo records it as one
  edit). Right-to-left keeps every older span's index valid.
- **Remote direction = full mirror.** Any Y.Text change not caused by this
  host's replay re-derives the whole document and replaces the host
  document. No index math on the remote side, ever.
- **Why NOT full-replace both directions** (an earlier draft, rejected and
  tested against): two concurrent whole-document replaces merge to a
  DUPLICATED document (X+X). Granular local deltas merge correctly under
  concurrency; the documented trade-off is per-span last-writer-wins on
  genuinely overlapping competing edits.
- **Safety model (verified against yjs 13.6.32):** Yjs fires type
  observers synchronously within the applying task (transact and
  applyUpdate both — probe-tested). CM6 dispatches run in their own
  tasks. Hence `after every task, host text === Y.Text`, the spans are
  valid Y coordinates at replay time, and peers converge — no
  re-anchoring state, no desynchronization bugs.

**Known limitation (documented, accepted):** two clients each rewriting the
entire document concurrently interleave both texts (any text CRDT does —
deletes merge, inserts both survive). Convergence itself (one document on
all peers, nothing lost) is invariant and test-pinned.

**Undo:** user-facing undo/redo is CodeMirror's `history()` extension
(remote mirrors are dispatched with `userEvent: 'input.remote'`, so they
do not pollute the local stack). The engine's y-undo is the programmatic
seam (LOCAL-origin only).

## S4 flip wiring (the mechanical part)

```ts
const eng = createEngine(projectId, {
  read: () => view.state.doc.toString(),
  write: (next: string) =>
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: next },
      userEvent: "input.remote",
    }),
});
// extensions: [..., view.history(), eng.cmBridge]
// teardown: eng.destroy(); view.destroy();
```

Retired at the flip (junk/): `editor/share-js-doc.ts`,
`editor/share-js-history-ot-type.ts`, `editor/history-ot.ts` (OT half),
`connection/connection-manager.ts` (socket.io half),
`source-editor/extensions/realtime.ts` (OT EditorFacade),
`frontend/js/vendor/libs/sharejs.js`, the `real-time` service, and the
document-updater OT path (ARC-1/ARC-2 verdicts).
