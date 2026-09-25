import * as Y from "yjs";

// LOCAL_ORIGIN / REMOTE_ORIGIN — origin markers. The CRDT merge does not
// care about origins, but UndoManager scoping and (future) per-origin
// filters do. Local = this client's host edit; remote = a state update
// (server relayed / another client).
export const LOCAL_ORIGIN = "ollitex-collab-local";
export const REMOTE_ORIGIN = "ollitex-collab-remote";

// LocalChange — one contiguous edit span in the OLD (pre-change) document
// coordinate space. A host (CodeMirror, via update.changes.iterChanges)
// yields these in ascending document order per edit.
export interface LocalChange {
  from: number;
  to: number;
  insert: string;
}

// TextSink — the host's editor, abstracted to a plain text model so the
// sync core stays CodeMirror-free and Node-testable. `read` is the current
// document (used by hosts/tests for assertions and by the remote mirror to
// detect no-ops); `write` atomically replaces the document (remote
// direction).
export interface TextSink {
  read: () => string;
  write: (next: string) => void;
}

// YTextSync — synchronizes the room's Y.Text (CRDT source of truth,
// persisted + versioned server-side by ygo) with a host editor.
//
// D24 (refined D20, recorded in WEB_GO_STATE.md) — the sync design:
//
//   LOCAL direction: GRANULAR delta ops. The host hands over its edit
//     spans (old-space coordinates); YTextSync replays them on the Y.Text
//     — right-to-left, delete-then-insert per span, in ONE transaction.
//     Right-to-left is what keeps every older span's index valid after the
//     newer spans are applied. This is the y-codemirror-correct pattern.
//
//   REMOTE direction: FULL MIRROR. Any Y.Text change not caused by this
//     host re-derives the whole document from the Y.Text and replaces the
//     host document. Remote application is always safe this way: there is
//     no index math on the remote side at all.
//
// Why NOT full-replacement in BOTH directions (an earlier draft of this
// file, rejected): two clients editing concurrently (both publishing a
// whole-document replace) merge to a DUPLICATED document (X+X) under the
// CRDT — the exact failure D19 forbids for seeding. Granular local deltas
// merge correctly under concurrency (disjoint edits interleave; overlapping
// competing edits resolve per-span last-writer-wins, the documented CRDT
// trade-off) and never duplicate.
//
// CORRECTNESS MODEL (verified against yjs 13.6.32 behavior):
//   * Yjs fires type observers SYNCHRONOUSLY within the task that applied
//     the change (probe-verified: transact and applyUpdate both emit
//     before returning). CodeMirror dispatches run in their own tasks.
//     Hence the invariant: AFTER EVERY TASK, host text === Y.Text text.
//     - remote task: update → observer mirrors host ← Y (same task);
//     - local task: edit → spans captured pre-dispatch → replays onto
//       Y.Text (same task) → Y === host.
//   * Because both directions converge to the SAME truth (the Y.Text) and
//     the host is always a pure projection of it, the peers converge —
//     there is no per-client editor state to diverge.
//   * Spans are captured in OLD-space coordinates (CM6 startState) while
//     the host (pre-edit) is exactly the old Y.Text (invariant), so the
//     spans are valid Y.Text coordinates at replay time — no index
//     re-anchoring, no relative-position state, no desynchronization
//     class of bugs.
//   * Re-entrancy guards (localDepth/remoteDepth) stop each direction
//     from re-entering the other mid-operation (local replay fires the
//     observer; the observer must not feed back into the replay).
export class YTextSync {
  private localDepth = 0;
  private remoteDepth = 0;
  private disposed = false;

  constructor(
    private readonly doc: Y.Doc,
    private readonly text: Y.Text,
    private readonly sink: TextSink,
  ) {
    this.text.observe(() => {
      if (!this.disposed) this.onRemote();
    });
  }

  // Local direction: host changed → replay its spans onto the Y.Text as
  // ONE transaction (so undo records it as one edit). Empty span list =
  // no-op (no transaction, no server version).
  applyLocal(spans: LocalChange[]): void {
    if (this.disposed || this.localDepth > 0 || this.remoteDepth > 0) {
      return;
    }
    if (spans.length === 0) return;
    this.localDepth++;
    try {
      this.doc.transact(() => {
        // Right-to-left so every yet-unapplied (older) span's index
        // stays valid (each applied span is to the LEFT of nothing
        // unapplied, and to the RIGHT of everything already applied).
        for (let i = spans.length - 1; i >= 0; i--) {
          const s = spans[i];
          if (s.to > s.from) this.text.delete(s.from, s.to - s.from);
          if (s.insert !== "") this.text.insert(s.from, s.insert);
        }
      }, LOCAL_ORIGIN);
    } finally {
      this.localDepth--;
    }
  }

  // Remote direction: Y.Text changed by something other than this host's
  // local replay → re-derive the host from the CRDT truth.
  private onRemote(): void {
    if (this.localDepth > 0 || this.remoteDepth > 0) return;
    this.remoteDepth++;
    try {
      const remote = this.text.toString();
      if (remote === this.sink.read()) return;
      this.sink.write(remote);
    } finally {
      this.remoteDepth--;
    }
  }

  destroy(): void {
    this.disposed = true;
  }
}
