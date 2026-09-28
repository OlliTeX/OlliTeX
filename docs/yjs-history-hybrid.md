# Yjs-native history hybrid (d5dd23dd) — design & slice plan

Approved scope (ledger D41, owner item 11, 2026-09-26; re-stated here for the
implementer):

> **history-v1: YES (hybrid b1).** Yjs-native history = the Y.Doc update
> stream; versions/restore already live (collabhistory REST + ygo versioned
> store + D23 retention). **diff** = Y.Text snapshots at two versions +
> go-diff (in-tree via otc). Multi-file hybrid: text in Y.Doc versions;
> file-tree ops (add/rename/delete/binary) stay in the Go web API with their
> own version log; the history contract composes both streams into the
> Node-parity answer. The OT engine behind it is transitional; retires after
> the last OT doc migrates.
>
> **Terminal state** = Yjs-native history (reusing history-v1.go's
> engine-agnostic HTTP/dispatch/security layer) + document-updater
> retirement + otc junked post-migration — **NOT** flipping the OT-engine
> ports live.

## Inventory (verified 2026-09-28, tree `5c89709299`)

| Plane | What | State |
|---|---|---|
| Yjs version plane | `go/services/collab` (ygo `persistence.VersionedPersistence`, mongo `ydoc` collection, room = project id, D23 `KeepVersions` retention) + `go/services/web/features/collabhistory` (REST: `/project/:pid/collab/history[/:v][/:v/restore]`, `/.../collab/doc`) | LIVE, e2e-green (collab-yjs) |
| OT legacy plane | `go/services/historyv1` (Go H1, :3100/api — full V1 API: initializeProject/cloneProject/deleteProject/snapshotAtVersion/getLatestContent/getContentAtVersion/getLatestHashedContent/getLatestHistory[Raw]/getHistory[Before]/… over otc OT chunks/blobs, mongo `projectHistoryChunks` + PG18) | LIVE (cmd/historyv1), otc consumer #1 |
| OT legacy plane | `go/services/project-history` (Go PH, :3054 — OT diff/snapshot math; consumer = Go web `features/history` v2Base) | LIVE (cmd/project-history), otc consumer #2 |
| Legacy service | Node DU `services/document-updater` (:3003) | residual live surface = 2: `POST .../change/accept` (trackchanges) + realtime `rt.FlushAPI` (documented "parity call; a no-op in practice") |
| Model | `go/libraries/otc` (89 files) | exactly 2 live consumers (H1, PH-appfactory) after stage-1 reduction |
| Web history UI | `features/history` = dual proxy (v1Base :3100/api + v2Base :3054) → editor panel hits `/project/:pid/updates` (frontend/js/features/history/services/api.ts) | LIVE, u101-gated |

## Slice plan (b1)

- **S1 — H1 backend swap (the core hybrid).** Same HTTP layer in `go/
  services/historyv1` (routes, dispatch, basic-auth security stay byte-pinned
  — u101 is the oracle), internals re-pointed at the Yjs plane via
  `go/services/collab`: versions = the room's ygo version index (sequential
  int ↔ Node-parity), content = `TextAt` snapshots, init = `SeedTextContent`,
  restore = `RestoreToVersion`, clone/delete = room clone/remove (add the
  small collab primitives if missing). OT chunk/blob code paths become
  unreachable (kept until S6 for rollback).
- **S2 — diff/changes surface.** `/changes` + diff 0–1 + file-tree diff:
  computed from two Y.Text snapshots + the go-diff port. **First move:**
  relocate the pure dmp/diff symbols out of `go/libraries/otc` into a neutral
  `go/libraries/textdiff` (pure, otc-free — unblocks otc junking), then
  `features/history`/H1-yjs compute diffs there; PH (:3054) is isolated as
  the last OT user.
- **S3 — file-tree stream.** Go web keeps its own version log for
  add/rename/delete/binary (new mongo collection, appended at the mutation
  sites: upload/entops/newzip/delent/delete paths) and the history contract
  composes both streams into the Node-parity `/updates` answer (chronological
  interleave, pinned by u101's file-tree cases).
- **S4 — DU retirement (D41-DU S3, now unblocked).** accept-changes per the
  recorded panel policy (panel already edits client-side in Y.Text =
  CRDT-convergent; server `/changes/accept` = state transition + room emit;
  the deterministic server-applied path uses an ordinary Y.Text mutation);
  drop the realtime `FlushAPI` (documented no-op parity shell); remove the
  `document-updater-overleaf` runit service, image build entry, env/compose
  references; move `services/document-updater` (Node) → `junk/`; e2e battery
  with DU **absent**.
- **S5 — PH retirement + otc junk.** `features/history` v2Base consumers
  retargeted (S1/S2 done) → PH service + its appfactory retargeted or
  retired; then the 9 pure symbols
  (EmptyHash, HexHashRxString, BlobHashFromBuffer, BlobHashFromString,
  GetStringLengthOfBuffer, ContainsNonBmpChars, Clean, IsClean, IsCleanDebug,
  MaxStringLength — 10 listed in the prior failure note; "9" counts the
  non-Hash pair) are localized into their consumers and `go/libraries/otc`
  → `junk/` (35ed23bd stage 2 complete).
- **S6 — terminal gates + ledger.** Full e2e battery (DU absent, OT plane
  retired), `make all` image rebuild + live-cycle verification, audit.

## Oracle gates (per slice, non-negotiable)

- **u101 matrix** (`specs/parity/web-go-u101-history`) — the dual-plane
  contract (OT V1 `/updates` + collab history) is the byte oracle for S1/S3.
- **review-panel** (D40 threads + tracked-changes surface + accept/reject
  flow) — S4 accept-retarget oracle.
- **collab-yjs** (+ realtime-bus) — the Yjs plane itself.
- **smoke** (editor + compile) — the app stays whole.
- `go test -race` full tree + the existing H1/PH unit matrices — green-slice
  per commit.
- Self-seeding batteries (`specs/parity/u101-history-matrix` etc.) for the
  fixture-regeneration class (see d74aa9fe memory).

## Known hazards (recorded)

1. **Version numbering**: Node parity expects sequential ints across BOTH
   streams (text + tree ops). The ygo version index is per-room; the
   composition layer must map merged streams → H1 ints deterministically
   (pin with u101's ordering cases).
2. **Hash semantics**: `/content?hash=…` pins OT blob hashes in some tests;
   the Yjs hash = content-bytes hash — verify each u101 case's expectation
   before claiming parity (the fixture-regeneration lesson applies).
3. **`{{var}}`/byte discipline unchanged**: H1's HTTP layer is byte-pinned —
   swap the backend, never the bytes.
4. **Do NOT** flip the OT engine ports "live" (owner decision): the OT code
   stays addressable until S5 removes its consumers; no runtime A/B.
