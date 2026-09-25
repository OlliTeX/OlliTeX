import type { Extension } from "@codemirror/state";
import type * as Y from "yjs";

import { syncExtension } from "./codemirror";
import { attachProviders, type Providers } from "./providers";
import { YTextSync, type TextSink } from "./sync";
import { TEXT_TYPE } from "./text-type";
import { newYContent, type YContent } from "./ydoc";

// YjsEngine — the complete D20/D24 client engine for one room (projectId):
//
//   doc      the Y.Doc (Y.Text TEXT_TYPE + programmatic UndoManager)
//   sync     the D24 Y.Text ↔ editor bridge (granular local / mirror remote)
//   ws       y-websocket provider → Go collab service /collab/<projectId>
//   idb      y-indexeddb offline persistence (same tab, across reloads)
//   cmBridge the CodeMirror-6 extension (local spans → Y.Text)
//
// Host contract (S4 flip wiring):
//   1. const eng = createEngine(projectId, sink)
//   2. view = new EditorView(... { extensions: [view.history(), ..., eng.cmBridge] })
//   3. the editor starts EMPTY (server is the single seed source, D19);
//      when the room's seed/update arrives over sync, the sink.write
//      mirror populates the document.
//   4. on teardown: eng.destroy() then view.destroy().
export interface YjsEngine {
  doc: Y.Doc;
  text: Y.Text;
  undo: Y.UndoManager;
  sync: YTextSync;
  providers: Providers;
  cmBridge: Extension;
  destroy: () => void;
}

export interface CreateEngineOptions {
  // ws:// base (defaults to the page origin). Tests pin this explicitly.
  wsBase?: string;
  // Skip provider attachment (pure local engine — unit tests, offline dev).
  offline?: boolean;
}

export function createEngine(
  projectId: string,
  sink: TextSink,
  opts: CreateEngineOptions = {},
): YjsEngine {
  const content: YContent = newYContent();
  const sync = new YTextSync(content.doc, content.text, sink);

  let providers: Providers = { ws: null, idb: null };
  if (!opts.offline) {
    providers = attachProviders(content.doc, projectId, opts.wsBase);
  }

  const cmBridge = syncExtension(sync);

  return {
    doc: content.doc,
    text: content.text,
    undo: content.undo,
    sync,
    providers,
    cmBridge,
    destroy: () => {
      sync.destroy();
      const p = providers as {
        ws?: { destroy?: () => void } | null;
        idb?: { destroy?: () => void; close?: () => void } | null;
      };
      if (p.ws && typeof p.ws.destroy === "function") p.ws.destroy();
      if (p.idb && typeof p.idb.destroy === "function") p.idb.destroy();
      else if (p.idb && typeof p.idb.close === "function") p.idb.close();
      content.doc.destroy();
    },
  };
}

export { TEXT_TYPE };
export type { TextSink, YContent };
