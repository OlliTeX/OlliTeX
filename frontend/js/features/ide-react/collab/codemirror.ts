import type { Extension } from "@codemirror/state";
import { EditorView, type ViewUpdate } from "@codemirror/view";

import type { LocalChange, YTextSync } from "./sync";

// spansOf — the host's edit as OLD-space change spans. This is the exact
// shape YTextSync.applyLocal consumes: CodeMirror's change set is the
// canonical local-edit description, and fromA/toA are the start-state
// (old) coordinates — the invariant's "old space". The repo-pinned
// @codemirror/state iterates all changes (callback-first signature).
function spansOf(update: ViewUpdate): LocalChange[] {
  const spans: LocalChange[] = [];
  update.changes.iterChanges((fromA, toA, _fromB, _toB, inserted) => {
    spans.push({ from: fromA, to: toA, insert: inserted.toString() });
  });
  return spans;
}

// syncExtension — the CodeMirror-6 half of the D24 yjs bridge: on any
// document change, replay the local edit spans onto the Y.Text via
// YTextSync (one transaction per edit). The remote direction lives in
// YTextSync and writes back through the sink (a CM6 dispatch with
// userEvent:'input.remote').
//
// Host wiring pattern (S4 flip):
//
//   const eng = createEngine(projectId, {
//     read: () => view.state.doc.toString(),
//     write: (next: string) => view.dispatch({
//       changes: { from: 0, to: view.state.doc.length, insert: next },
//       userEvent: 'input.remote',
//     }),
//   })
//   // view = new EditorView({ state, extensions: [..., view.history(), eng.cmBridge] })
//
// `userEvent: 'input.remote'` keeps remote mirrors OUT of CM6's undo
// stack: user-facing undo/redo is CM6's own `history()` extension (D24 —
// CM6 owns interactive undo; the engine's y-undo is the programmatic
// seam). Local edits are the only things Ctrl+Z can walk.
export function syncExtension(sync: YTextSync): Extension {
  return EditorView.updateListener.of((update) => {
    if (update.docChanged) sync.applyLocal(spansOf(update));
  });
}
