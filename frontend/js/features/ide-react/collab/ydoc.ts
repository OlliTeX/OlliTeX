import * as Y from "yjs";

import { LOCAL_ORIGIN } from "./sync";
import { TEXT_TYPE } from "./text-type";

export interface YContent {
  doc: Y.Doc;
  text: Y.Text;
  undo: Y.UndoManager;
}

// newYContent — the minimal room document: one Y.Text (TEXT_TYPE) plus a
// programmatic UndoManager over the whole text.
//
// Undo semantics (D24): the UndoManager tracks ONLY this client's LOCAL
// edits (LOCAL_ORIGIN) — yjs defaults to tracking null-origin transactions
// only; remote updates arrive under their own origins (the server relay
// peer/doc identity) and therefore stay OUT of the programmatic stack.
// User-facing undo is CM6's own history() extension; this UndoManager is
// the programmatic seam (e.g. reverting an AI suggestion made locally).
//
// NOTE (D19 seeding rule): this NEVER pre-fills content. The server is the
// single source of the first content (ygo `OnLoadDocument` + SeedFn) — a
// peer joins EMPTY and receives the seed during initial sync. Client-side
// pre-fill is forbidden (two peers pre-filling with different client IDs
// would merge to X+X).
export function newYContent(doc?: Y.Doc): YContent {
  const ydoc = doc ?? new Y.Doc();
  const text = ydoc.getText(TEXT_TYPE);
  const undo = new Y.UndoManager(text, {
    trackedOrigins: new Set([LOCAL_ORIGIN]),
  });
  return { doc: ydoc, text, undo };
}
